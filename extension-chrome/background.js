// Surge Download Manager - Background Service Worker
// Intercepts downloads and sends them to local Surge instance

const DEFAULT_PORT = 8080;
const MAX_PORT_SCAN = 100;
const INTERCEPT_ENABLED_KEY = "interceptEnabled";

// === State ===
let cachedPort = null;
let downloads = new Map();
let lastHealthCheck = 0;
let isConnected = false;

// Pending duplicate downloads waiting for user confirmation
// Key: unique id, Value: { downloadItem, filename, directory, timestamp }
const pendingDuplicates = new Map();
let pendingDuplicateCounter = 0;

function updateBadge() {
  const count = pendingDuplicates.size;
  if (count > 0) {
    chrome.action.setBadgeText({ text: count.toString() });
    chrome.action.setBadgeBackgroundColor({ color: "#FF0000" }); // Red
  } else {
    chrome.action.setBadgeText({ text: "" });
  }
}

// === Header Capture ===
// Store request headers for URLs to forward to Surge (cookies, auth, etc.)
// Key: URL, Value: { headers: {}, timestamp: Date.now() }
const capturedHeaders = new Map();
const HEADER_EXPIRY_MS = 120000; // 2 minutes - headers expire after this time

// Capture all headers from requests using webRequest API
chrome.webRequest.onBeforeSendHeaders.addListener(
  (details) => {
    if (!details.requestHeaders || !details.url) return;

    // Capture all headers
    const headers = {};
    for (const header of details.requestHeaders) {
      headers[header.name] = header.value;
    }

    // Only store if we captured something
    if (Object.keys(headers).length > 0) {
      capturedHeaders.set(details.url, {
        headers,
        timestamp: Date.now(),
      });

      // Cleanup old entries periodically
      if (capturedHeaders.size > 1000) {
        cleanupExpiredHeaders();
      }
    }
  },
  { urls: ["<all_urls>"] },
  ["requestHeaders", "extraHeaders"],
);

function cleanupExpiredHeaders() {
  const now = Date.now();
  for (const [url, data] of capturedHeaders) {
    if (now - data.timestamp > HEADER_EXPIRY_MS) {
      capturedHeaders.delete(url);
    }
  }
}

function getCapturedHeaders(url) {
  const data = capturedHeaders.get(url);
  if (!data) return null;

  // Check if expired
  if (Date.now() - data.timestamp > HEADER_EXPIRY_MS) {
    capturedHeaders.delete(url);
    return null;
  }

  return data.headers;
}

// === Port Discovery ===

async function findSurgePort() {
  // Try cached port first (with quick timeout)
  if (cachedPort) {
    try {
      const response = await fetch(`http://127.0.0.1:${cachedPort}/health`, {
        method: "GET",
        signal: AbortSignal.timeout(300),
      });
      if (response.ok) {
        isConnected = true;
        return cachedPort;
      }
    } catch {}
    cachedPort = null;
  }

  // Scan for available port
  for (let port = DEFAULT_PORT; port < DEFAULT_PORT + MAX_PORT_SCAN; port++) {
    try {
      const response = await fetch(`http://127.0.0.1:${port}/health`, {
        method: "GET",
        signal: AbortSignal.timeout(200),
      });
      if (response.ok) {
        cachedPort = port;
        isConnected = true;
        console.log(`[Surge] Found server on port ${port}`);
        return port;
      }
    } catch {}
  }

  isConnected = false;
  return null;
}

async function checkSurgeHealth() {
  const now = Date.now();
  // Rate limit health checks to once per second
  if (now - lastHealthCheck < 1000) {
    return isConnected;
  }
  lastHealthCheck = now;

  const port = await findSurgePort();
  isConnected = port !== null;
  return isConnected;
}

// === Download List Fetching ===

async function fetchDownloadList() {
  const port = await findSurgePort();
  if (!port) {
    return [];
  }

  try {
    const response = await fetch(`http://127.0.0.1:${port}/list`, {
      method: "GET",
      signal: AbortSignal.timeout(5000),
    });

    if (response.ok) {
      const list = await response.json();

      // Handle null or non-array response
      if (!Array.isArray(list)) {
        return [];
      }

      // Calculate ETA for each download
      return list.map((dl) => {
        let eta = null;
        if (dl.status === "downloading" && dl.speed > 0 && dl.total_size > 0) {
          const remaining = dl.total_size - dl.downloaded;
          // Speed is in MB/s, convert to bytes/s
          const speedBytes = dl.speed * 1024 * 1024;
          eta = Math.ceil(remaining / speedBytes);
        }
        return { ...dl, eta };
      });
    }
  } catch (error) {
    console.error("[Surge] Error fetching downloads:", error);
  }

  return [];
}

// === Download Sending ===

async function sendToSurge(url, filename, absolutePath) {
  const port = await findSurgePort();
  if (!port) {
    console.error("[Surge] No server found");
    return { success: false, error: "Server not running" };
  }

  try {
    const body = {
      url: url,
      filename: filename || "",
    };

    // Use absolute path directly if provided
    if (absolutePath) {
      body.path = absolutePath;
    }

    // Include captured headers for authenticated downloads
    const headers = getCapturedHeaders(url);
    if (headers) {
      body.headers = headers;
      console.log("[Surge] Forwarding captured headers to Surge");
    }

    // Always skip TUI approval for extension downloads (vetted by user action)
    // This also bypasses duplicate warnings since extension handles those
    body.skip_approval = true;

    const response = await fetch(`http://127.0.0.1:${port}/download`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
    });

    if (response.ok) {
      const data = await response.json();
      console.log("[Surge] Download queued:", data);
      return { success: true, data };
    } else {
      const error = await response.text();
      console.error(
        "[Surge] Failed to queue download:",
        response.status,
        error,
      );
      return { success: false, error };
    }
  } catch (error) {
    console.error("[Surge] Error sending to Surge:", error);
    return { success: false, error: error.message };
  }
}

// === Download Control ===

async function pauseDownload(id) {
  const port = await findSurgePort();
  if (!port) return false;

  try {
    const response = await fetch(`http://127.0.0.1:${port}/pause?id=${id}`, {
      method: "POST",
      signal: AbortSignal.timeout(5000),
    });
    return response.ok;
  } catch (error) {
    console.error("[Surge] Error pausing download:", error);
    return false;
  }
}

async function resumeDownload(id) {
  const port = await findSurgePort();
  if (!port) return false;

  try {
    const response = await fetch(`http://127.0.0.1:${port}/resume?id=${id}`, {
      method: "POST",
      signal: AbortSignal.timeout(5000),
    });
    return response.ok;
  } catch (error) {
    console.error("[Surge] Error resuming download:", error);
    return false;
  }
}

async function cancelDownload(id) {
  const port = await findSurgePort();
  if (!port) return false;

  try {
    const response = await fetch(`http://127.0.0.1:${port}/delete?id=${id}`, {
      method: "DELETE",
      signal: AbortSignal.timeout(5000),
    });
    return response.ok;
  } catch (error) {
    console.error("[Surge] Error canceling download:", error);
    return false;
  }
}

// === Interception State ===

async function isInterceptEnabled() {
  const result = await chrome.storage.local.get(INTERCEPT_ENABLED_KEY);
  return result[INTERCEPT_ENABLED_KEY] !== false;
}

// === Deduplication ===

// Check if URL is already being downloaded by Surge
async function isDuplicateDownload(url) {
  try {
    const downloadsList = await fetchDownloadList();
    if (downloadsList && downloadsList.length > 0) {
      const normalizedUrl = url.replace(/\/$/, ""); // Remove trailing slash
      for (const dl of downloadsList) {
        const normalizedDlUrl = (dl.url || "").replace(/\/$/, "");
        // Flag as duplicate if URL exists in Surge's download list (any status)
        if (normalizedDlUrl === normalizedUrl) {
          console.log(
            "[Surge] Duplicate download detected (already in Surge):",
            url,
          );
          return true;
        }
      }
    }
  } catch (e) {
    console.log("[Surge] Could not check Surge list for duplicates:", e);
  }

  return false;
}

function isFreshDownload(downloadItem) {
  // Must be in progress (not completed/interrupted from history)
  if (downloadItem.state && downloadItem.state !== "in_progress") {
    return false;
  }

  // Check start time
  if (!downloadItem.startTime) return true;

  const startTime = new Date(downloadItem.startTime).getTime();
  const now = Date.now();
  const diff = now - startTime;

  // If download started more than 30 seconds ago, likely history sync
  if (diff > 30000) {
    return false;
  }

  return true;
}

function shouldSkipUrl(url) {
  // Skip blob and data URLs
  if (url.startsWith("blob:") || url.startsWith("data:")) {
    return true;
  }

  // Skip chrome extension URLs
  if (url.startsWith("chrome-extension:") || url.startsWith("moz-extension:")) {
    return true;
  }

  return false;
}

// === Path Extraction ===

function extractPathInfo(downloadItem) {
  let filename = "";
  let directory = "";

  if (downloadItem.filename) {
    // downloadItem.filename contains the full path chosen by user
    // On Windows: C:\Users\Name\Downloads\file.zip
    // On macOS/Linux: /home/user/Downloads/file.zip

    const fullPath = downloadItem.filename;

    // Normalize separators and split
    const normalized = fullPath.replace(/\\/g, "/");
    const parts = normalized.split("/");

    filename = parts.pop() || "";

    if (parts.length > 0) {
      // Reconstruct directory path
      // On Windows, we need to preserve the drive letter
      if (/^[A-Za-z]:$/.test(parts[0])) {
        // Windows path with drive letter
        directory = parts.join("/");
      } else if (parts[0] === "") {
        // Unix absolute path (starts with /)
        directory = "/" + parts.slice(1).join("/");
      } else {
        directory = parts.join("/");
      }
    }
  }

  return { filename, directory };
}

// === Download Interception ===
// Two-phase approach to properly capture user-selected path from Save As dialog:
// 1. onCreated: Store the download as "pending" if filename not yet determined
// 2. onChanged: Wait for filename to be determined (after Save As dialog), then intercept

const processedIds = new Set();

chrome.downloads.onCreated.addListener(async (downloadItem) => {
  // Prevent duplicate events for the same download ID
  if (processedIds.has(downloadItem.id)) {
    return;
  }

  console.log(
    "[Surge] Download created:",
    downloadItem.url,
    "filename:",
    downloadItem.filename,
    "state:",
    downloadItem.state,
  );

  // Quick checks that can be done immediately
  const enabled = await isInterceptEnabled();
  if (!enabled) {
    console.log("[Surge] Interception disabled");
    return;
  }

  if (shouldSkipUrl(downloadItem.url)) {
    console.log("[Surge] Skipping URL type");
    return;
  }

  if (!isFreshDownload(downloadItem)) {
    console.log("[Surge] Ignoring historical download");
    return;
  }

  // Intercept immediately - we don't wait for Save As / filenames anymore
  // as user requested to force default directory for everything.
  processedIds.add(downloadItem.id);
  setTimeout(() => processedIds.delete(downloadItem.id), 120000);

  await handleDownloadIntercept(downloadItem);
});

async function handleDownloadIntercept(downloadItem) {
  // Check for duplicates (async - checks both time-based and Surge's download list)
  if (await isDuplicateDownload(downloadItem.url)) {
    // Cancel the browser download
    try {
      await chrome.downloads.cancel(downloadItem.id);
      await chrome.downloads.erase({ id: downloadItem.id });
    } catch (e) {
      console.log("[Surge] Error canceling duplicate:", e);
    }

    // Store pending duplicate and prompt user
    const pendingId = `dup_${++pendingDuplicateCounter}`;
    const { filename, directory } = extractPathInfo(downloadItem);
    const displayName =
      filename || downloadItem.url.split("/").pop() || "Unknown file";

    pendingDuplicates.set(pendingId, {
      downloadItem,
      filename,
      directory,
      url: downloadItem.url,
      timestamp: Date.now(),
    });

    // Cleanup old pending duplicates (older than 60s)
    for (const [id, data] of pendingDuplicates) {
      if (Date.now() - data.timestamp > 60000) {
        pendingDuplicates.delete(id);
        updateBadge();
      }
    }

    // Update badge
    updateBadge();

    // Try to open popup and send prompt (might fail if no user gesture)
    try {
      await chrome.action.openPopup();
    } catch (e) {
      console.log("[Surge] openPopup failed (might be open or no user gesture):", e);
    }

    // Send message to popup
    chrome.runtime
      .sendMessage({
        type: "promptDuplicate",
        id: pendingId,
        filename: displayName,
      })
      .catch((e) => {
        // Popup might not be open, that's ok - duplicate will timeout
        console.log("[Surge] Sending promptDuplicate failed:", e);
      });

    return;
  }

  // Check if Surge is running
  const surgeRunning = await checkSurgeHealth();
  if (!surgeRunning) {
    console.log("[Surge] Server not running, using browser download");
    return; // Let browser continue - download is already in progress
  }

  // Extract path info - filename now contains the full path from Save As dialog
  const { filename } = extractPathInfo(downloadItem);

  console.log(
    "[Surge] Extracted path info - filename:",
    filename,
    "directory: (forced default)",
  );

  // Cancel browser download and send to Surge
  try {
    await chrome.downloads.cancel(downloadItem.id);
    await chrome.downloads.erase({ id: downloadItem.id });

    // Force default directory by passing empty string
    const result = await sendToSurge(downloadItem.url, filename, "");

    if (result.success) {
      if (result.data && result.data.status === "pending_approval") {
        chrome.notifications.create(`surge-confirm-${downloadItem.id}`, {
          type: "basic",
          iconUrl: "icons/icon48.png",
          title: "Surge - Confirmation Required",
          message: `Click to confirm download: ${filename || downloadItem.url.split("/").pop()}`,
          requireInteraction: true,
        });
        return; // Don't auto-open popup for pending interactions
      }

      // Show notification
      chrome.notifications.create({
        type: "basic",
        iconUrl: "icons/icon48.png",
        title: "Surge",
        message: `Download started: ${filename || downloadItem.url.split("/").pop()}`,
      });

      // Auto-open the popup to show download progress
      try {
        await chrome.action.openPopup();
      } catch (e) {
        // openPopup may fail if popup is already open or no user gesture
        console.log("[Surge] Could not auto-open popup:", e.message);
      }
    } else {
      // Failed to send to Surge - show error notification
      chrome.notifications.create({
        type: "basic",
        iconUrl: "icons/icon48.png",
        title: "Surge Error",
        message: `Failed to start download: ${result.error}`,
      });
    }
  } catch (error) {
    console.error("[Surge] Failed to intercept download:", error);
  }
}

// Handle notification clicks
chrome.notifications.onClicked.addListener((notificationId) => {
  if (notificationId.startsWith("surge-confirm-")) {
    // Attempt to open popup
    try {
      chrome.action.openPopup();
    } catch (e) {
      console.error("[Surge] Failed to open popup from notification:", e);
    }
    // Clear notification
    chrome.notifications.clear(notificationId);
  }
});

// === Message Handling ===

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  // Handle async responses
  (async () => {
    try {
      switch (message.type) {
        case "checkHealth": {
          const healthy = await checkSurgeHealth();
          sendResponse({ healthy });
          break;
        }

        case "getStatus": {
          const enabled = await isInterceptEnabled();
          sendResponse({ enabled });
          break;
        }

        case "setStatus": {
          await chrome.storage.local.set({
            [INTERCEPT_ENABLED_KEY]: message.enabled,
          });
          sendResponse({ success: true });
          break;
        }

        case "getDownloads": {
          const downloadsList = await fetchDownloadList();
          sendResponse({
            downloads: downloadsList,
            connected: isConnected,
          });
          break;
        }

        case "pauseDownload": {
          const success = await pauseDownload(message.id);
          sendResponse({ success });
          break;
        }

        case "resumeDownload": {
          const success = await resumeDownload(message.id);
          sendResponse({ success });
          break;
        }

        case "cancelDownload": {
          const success = await cancelDownload(message.id);
          sendResponse({ success });
          break;
        }

        case "confirmDuplicate": {
          // User confirmed duplicate download
          const pending = pendingDuplicates.get(message.id);
          console.log(
            "[Surge] confirmDuplicate called, pending:",
            pending ? "found" : "NOT FOUND",
            "id:",
            message.id,
          );
          if (pending) {
            pendingDuplicates.delete(message.id);
            updateBadge(); // Update badge

            console.log(
              "[Surge] Sending confirmed duplicate to Surge:",
              pending.url,
            );
            const result = await sendToSurge(
              pending.url,
              pending.filename,
              pending.directory,
            );
            console.log("[Surge] sendToSurge result:", result);

            if (result.success) {
              chrome.notifications.create({
                type: "basic",
                iconUrl: "icons/icon48.png",
                title: "Surge",
                message: `Download started: ${pending.filename || pending.url.split("/").pop()}`,
              });
            }

            // Check for next pending duplicate
            if (pendingDuplicates.size > 0) {
              const [nextId, nextData] = pendingDuplicates.entries().next().value;
              const nextName = nextData.filename || nextData.url.split("/").pop() || "Unknown file";
              
              chrome.runtime.sendMessage({
                type: "promptDuplicate",
                id: nextId,
                filename: nextName,
              }).catch(() => {});
            }

            sendResponse({ success: result.success });
          } else {
            sendResponse({
              success: false,
              error: "Pending download not found",
            });
          }
          break;
        }

        case "skipDuplicate": {
          // User skipped duplicate download
          const pending = pendingDuplicates.get(message.id);
          if (pending) {
            pendingDuplicates.delete(message.id);
            updateBadge(); // Update badge

            console.log(
              "[Surge] User skipped duplicate download:",
              pending.url,
            );
            
            // Check for next pending duplicate
            if (pendingDuplicates.size > 0) {
              const [nextId, nextData] = pendingDuplicates.entries().next().value;
              const nextName = nextData.filename || nextData.url.split("/").pop() || "Unknown file";
              
              chrome.runtime.sendMessage({
                type: "promptDuplicate",
                id: nextId,
                filename: nextName,
              }).catch(() => {});
            }
          }
          sendResponse({ success: true });
          break;
        }

        case "getPendingDuplicates": {
          const duplicates = [];
          for (const [id, data] of pendingDuplicates) {
            duplicates.push({
              id,
              filename:
                data.filename || data.url.split("/").pop() || "Unknown file",
              url: data.url,
            });
          }
          sendResponse({ duplicates });
          break;
        }

        default:
          sendResponse({ error: "Unknown message type" });
      }
    } catch (error) {
      console.error("[Surge] Message handler error:", error);
      sendResponse({ error: error.message });
    }
  })();

  return true; // Keep channel open for async response
});

// === Initialization ===

async function initialize() {
  console.log("[Surge] Extension initializing...");
  await checkSurgeHealth();
  console.log("[Surge] Extension loaded");
}

initialize();
