// Surge Download Manager - Background Script (Firefox)
// Intercepts downloads and sends them to local Surge instance

const DEFAULT_PORT = 8080;
const MAX_PORT_SCAN = 100;
const INTERCEPT_ENABLED_KEY = 'interceptEnabled';
const SEEN_DOWNLOADS_KEY = 'seenDownloads';
const DEDUP_WINDOW_MS = 5000; // 5 seconds - catch duplicate events, allow intentional re-downloads

// === State ===
let cachedPort = null;
let downloads = new Map();
let lastHealthCheck = 0;
let isConnected = false;

// Deduplication: URL hash -> timestamp
const recentDownloads = new Map();

// Pending duplicate downloads waiting for user confirmation
// Key: unique id, Value: { downloadItem, filename, directory, timestamp }
const pendingDuplicates = new Map();
let pendingDuplicateCounter = 0;

// === Port Discovery ===

async function findSurgePort() {
  // Try cached port first (with quick timeout)
  if (cachedPort) {
    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 300);
      const response = await fetch(`http://127.0.0.1:${cachedPort}/health`, {
        method: 'GET',
        signal: controller.signal,
      });
      clearTimeout(timeoutId);
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
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 200);
      const response = await fetch(`http://127.0.0.1:${port}/health`, {
        method: 'GET',
        signal: controller.signal,
      });
      clearTimeout(timeoutId);
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
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), 5000);
    const response = await fetch(`http://127.0.0.1:${port}/list`, {
      method: 'GET',
      signal: controller.signal,
    });
    clearTimeout(timeoutId);
    
    if (response.ok) {
      const list = await response.json();
      
      // Handle null or non-array response
      if (!Array.isArray(list)) {
        return [];
      }
      
      // Calculate ETA for each download
      return list.map(dl => {
        let eta = null;
        if (dl.status === 'downloading' && dl.speed > 0 && dl.total_size > 0) {
          const remaining = dl.total_size - dl.downloaded;
          // Speed is in MB/s, convert to bytes/s
          const speedBytes = dl.speed * 1024 * 1024;
          eta = Math.ceil(remaining / speedBytes);
        }
        return { ...dl, eta };
      });
    }
  } catch (error) {
    console.error('[Surge] Error fetching downloads:', error);
  }
  
  return [];
}

// === Download Sending ===

async function sendToSurge(url, filename, absolutePath) {
  const port = await findSurgePort();
  if (!port) {
    console.error('[Surge] No server found');
    return { success: false, error: 'Server not running' };
  }

  try {
    const body = {
      url: url,
      filename: filename || '',
    };

    // Use absolute path directly if provided
    if (absolutePath) {
      body.path = absolutePath;
    }

    // Always skip TUI approval for extension downloads (vetted by user action)
    // This also bypasses duplicate warnings since extension handles those
    body.skip_approval = true;

    const response = await fetch(`http://127.0.0.1:${port}/download`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(body),
    });

    if (response.ok) {
      const data = await response.json();
      console.log('[Surge] Download queued:', data);
      return { success: true, data };
    } else {
      const error = await response.text();
      console.error('[Surge] Failed to queue download:', response.status, error);
      return { success: false, error };
    }
  } catch (error) {
    console.error('[Surge] Error sending to Surge:', error);
    return { success: false, error: error.message };
  }
}

// === Download Control ===

async function pauseDownload(id) {
  const port = await findSurgePort();
  if (!port) return false;

  try {
    const response = await fetch(`http://127.0.0.1:${port}/pause?id=${id}`, {
      method: 'POST',
    });
    return response.ok;
  } catch (error) {
    console.error('[Surge] Error pausing download:', error);
    return false;
  }
}

async function resumeDownload(id) {
  const port = await findSurgePort();
  if (!port) return false;

  try {
    const response = await fetch(`http://127.0.0.1:${port}/resume?id=${id}`, {
      method: 'POST',
    });
    return response.ok;
  } catch (error) {
    console.error('[Surge] Error resuming download:', error);
    return false;
  }
}

async function cancelDownload(id) {
  const port = await findSurgePort();
  if (!port) return false;

  try {
    const response = await fetch(`http://127.0.0.1:${port}/delete?id=${id}`, {
      method: 'DELETE',
    });
    return response.ok;
  } catch (error) {
    console.error('[Surge] Error canceling download:', error);
    return false;
  }
}

// === Interception State ===

async function isInterceptEnabled() {
  const result = await browser.storage.local.get(INTERCEPT_ENABLED_KEY);
  return result[INTERCEPT_ENABLED_KEY] !== false;
}

// === Deduplication ===

function hashUrl(url) {
  let hash = 0;
  for (let i = 0; i < url.length; i++) {
    const char = url.charCodeAt(i);
    hash = ((hash << 5) - hash) + char;
    hash = hash & hash;
  }
  return hash.toString(36);
}

// Check if URL is duplicate (either in recent downloads OR in Surge's active list)
async function isDuplicateDownload(url) {
  const hash = hashUrl(url);
  const now = Date.now();
  
  // Check 1: Time-based (catches rapid double-clicks within 5 seconds)
  if (recentDownloads.has(hash)) {
    const lastSeen = recentDownloads.get(hash);
    if (now - lastSeen < DEDUP_WINDOW_MS) {
      console.log('[Surge] Duplicate download detected (time-based):', url);
      return true;
    }
  }
  
  // Cleanup old entries
  for (const [key, timestamp] of recentDownloads) {
    if (now - timestamp > DEDUP_WINDOW_MS) {
      recentDownloads.delete(key);
    }
  }
  
  // Check 2: Query Surge's active download list
  try {
    const downloadsList = await fetchDownloadList();
    if (downloadsList && downloadsList.length > 0) {
      const normalizedUrl = url.replace(/\/$/, ''); // Remove trailing slash
      for (const dl of downloadsList) {
        const normalizedDlUrl = (dl.url || '').replace(/\/$/, '');
        if (normalizedDlUrl === normalizedUrl && dl.status !== 'completed') {
          console.log('[Surge] Duplicate download detected (in Surge list):', url);
          return true;
        }
      }
    }
  } catch (e) {
    console.log('[Surge] Could not check Surge list for duplicates:', e);
  }
  
  return false;
}

function markDownloadSeen(url) {
  const hash = hashUrl(url);
  recentDownloads.set(hash, Date.now());
}

// === History Filtering ===

async function markExistingDownloads() {
  try {
    const history = await browser.downloads.search({
      limit: 100,
      orderBy: ['-startTime'],
    });
    
    const seenUrls = {};
    const now = Date.now();
    
    history.forEach(item => {
      if (item.url && !item.url.startsWith('blob:') && !item.url.startsWith('data:')) {
        const hash = hashUrl(item.url);
        seenUrls[hash] = now;
        recentDownloads.set(hash, now);
      }
    });
    
    await browser.storage.local.set({ [SEEN_DOWNLOADS_KEY]: seenUrls });
    console.log(`[Surge] Marked ${Object.keys(seenUrls).length} existing downloads`);
  } catch (error) {
    console.error('[Surge] Error marking existing downloads:', error);
  }
}

function isFreshDownload(downloadItem) {
  if (downloadItem.state && downloadItem.state !== 'in_progress') {
    return false;
  }

  if (!downloadItem.startTime) return true;

  const startTime = new Date(downloadItem.startTime).getTime();
  const now = Date.now();
  const diff = now - startTime;

  if (diff > 30000) {
    return false;
  }
  
  return true;
}

function shouldSkipUrl(url) {
  if (url.startsWith('blob:') || url.startsWith('data:')) {
    return true;
  }
  
  if (url.startsWith('chrome-extension:') || url.startsWith('moz-extension:')) {
    return true;
  }
  
  return false;
}

// === Path Extraction ===

function extractPathInfo(downloadItem) {
  let filename = '';
  let directory = '';

  if (downloadItem.filename) {
    const fullPath = downloadItem.filename;
    const normalized = fullPath.replace(/\\/g, '/');
    const parts = normalized.split('/');
    
    filename = parts.pop() || '';
    
    if (parts.length > 0) {
      if (/^[A-Za-z]:$/.test(parts[0])) {
        directory = parts.join('/');
      } else if (parts[0] === '') {
        directory = '/' + parts.slice(1).join('/');
      } else {
        directory = parts.join('/');
      }
    }
  }

  return { filename, directory };
}

// === Download Interception ===
// Firefox doesn't support onDeterminingFilename, so we use a two-phase approach:
// 1. onCreated: Store the download as "pending" 
// 2. onChanged: Wait for filename to be determined, then intercept

const processedIds = new Set();
const pendingInterceptions = new Map(); // downloadId -> downloadItem

browser.downloads.onCreated.addListener(async (downloadItem) => {
  if (processedIds.has(downloadItem.id)) {
    return;
  }
  
  console.log('[Surge] Download created:', downloadItem.url);

  const enabled = await isInterceptEnabled();
  if (!enabled) {
    console.log('[Surge] Interception disabled');
    return;
  }

  if (shouldSkipUrl(downloadItem.url)) {
    console.log('[Surge] Skipping URL type');
    return;
  }

  if (!isFreshDownload(downloadItem)) {
    console.log('[Surge] Ignoring historical download');
    return;
  }

  // If filename is already determined (auto-download mode), intercept immediately
  if (downloadItem.filename && downloadItem.filename.length > 0) {
    console.log('[Surge] Filename already determined, intercepting immediately');
    processedIds.add(downloadItem.id);
    setTimeout(() => processedIds.delete(downloadItem.id), 120000);
    await handleDownloadIntercept(downloadItem);
    return;
  }

  // Otherwise, wait for filename to be determined (Save As dialog)
  console.log('[Surge] Waiting for filename determination...');
  pendingInterceptions.set(downloadItem.id, downloadItem);
  
  // Set timeout to cleanup if filename never gets determined
  setTimeout(() => {
    if (pendingInterceptions.has(downloadItem.id)) {
      console.log('[Surge] Timeout waiting for filename, cleaning up');
      pendingInterceptions.delete(downloadItem.id);
    }
  }, 60000);
});

// Listen for download changes to catch when filename is determined
browser.downloads.onChanged.addListener(async (delta) => {
  // Check if this download is pending interception
  if (!pendingInterceptions.has(delta.id)) {
    return;
  }

  // Check if filename was just determined
  if (delta.filename && delta.filename.current) {
    console.log('[Surge] Filename determined:', delta.filename.current);
    
    const downloadItem = pendingInterceptions.get(delta.id);
    pendingInterceptions.delete(delta.id);
    
    // Mark as processed
    if (processedIds.has(delta.id)) {
      return;
    }
    processedIds.add(delta.id);
    setTimeout(() => processedIds.delete(delta.id), 120000);
    
    // Update the downloadItem with the new filename
    downloadItem.filename = delta.filename.current;
    
    await handleDownloadIntercept(downloadItem);
  }
  
  // If download was cancelled or errored, clean up
  if (delta.state && (delta.state.current === 'interrupted' || delta.state.current === 'complete')) {
    pendingInterceptions.delete(delta.id);
  }
});

async function handleDownloadIntercept(downloadItem) {
  // Check for duplicates (async - checks both time-based and Surge's download list)
  if (await isDuplicateDownload(downloadItem.url)) {
    // Cancel the browser download
    try {
      await browser.downloads.cancel(downloadItem.id);
      await browser.downloads.erase({ id: downloadItem.id });
    } catch (e) {
      console.log('[Surge] Error canceling duplicate:', e);
    }
    
    // Store pending duplicate and prompt user
    const pendingId = `dup_${++pendingDuplicateCounter}`;
    const { filename, directory } = extractPathInfo(downloadItem);
    const displayName = filename || downloadItem.url.split('/').pop() || 'Unknown file';
    
    pendingDuplicates.set(pendingId, {
      downloadItem,
      filename,
      directory,
      url: downloadItem.url,
      timestamp: Date.now()
    });
    
    // Cleanup old pending duplicates (older than 60s)
    for (const [id, data] of pendingDuplicates) {
      if (Date.now() - data.timestamp > 60000) {
        pendingDuplicates.delete(id);
      }
    }
    
    // Try to open popup and send prompt
    try {
      await browser.action.openPopup();
    } catch (e) {
      // Popup may already be open
    }
    
    // Send message to popup
    browser.runtime.sendMessage({
      type: 'promptDuplicate',
      id: pendingId,
      filename: displayName
    }).catch(() => {
      // Popup might not be open, that's ok - duplicate will timeout
    });
    
    return;
  }
  
  // Mark as seen for future duplicate detection
  markDownloadSeen(downloadItem.url);

  const surgeRunning = await checkSurgeHealth();
  if (!surgeRunning) {
    console.log('[Surge] Server not running, using browser download');
    recentDownloads.delete(hashUrl(downloadItem.url));
    return; // Let browser continue - download is already in progress
  }

  const { filename, directory } = extractPathInfo(downloadItem);

  try {
    await browser.downloads.cancel(downloadItem.id);
    await browser.downloads.erase({ id: downloadItem.id });

    const result = await sendToSurge(
      downloadItem.url,
      filename,
      directory
    );

    if (result.success) {
      browser.notifications.create({
        type: 'basic',
        iconUrl: 'icons/icon48.png',
        title: 'Surge',
        message: `Download started: ${filename || downloadItem.url.split('/').pop()}`,
      });
      
      // Auto-open the popup to show download progress
      try {
        await browser.action.openPopup();
      } catch (e) {
        // openPopup may fail if popup is already open or no user gesture
        console.log('[Surge] Could not auto-open popup:', e.message);
      }
    } else {
      browser.notifications.create({
        type: 'basic',
        iconUrl: 'icons/icon48.png',
        title: 'Surge Error',
        message: `Failed to start download: ${result.error}`,
      });
    }
  } catch (error) {
    console.error('[Surge] Failed to intercept download:', error);
  }
}

// === Message Handling ===

browser.runtime.onMessage.addListener((message, sender) => {
  return (async () => {
    try {
      switch (message.type) {
        case 'checkHealth': {
          const healthy = await checkSurgeHealth();
          return { healthy };
        }
        
        case 'getStatus': {
          const enabled = await isInterceptEnabled();
          return { enabled };
        }
        
        case 'setStatus': {
          await browser.storage.local.set({ [INTERCEPT_ENABLED_KEY]: message.enabled });
          return { success: true };
        }
        
        case 'getDownloads': {
          const downloadsList = await fetchDownloadList();
          return { 
            downloads: downloadsList, 
            connected: isConnected 
          };
        }
        
        case 'pauseDownload': {
          const success = await pauseDownload(message.id);
          return { success };
        }
        
        case 'resumeDownload': {
          const success = await resumeDownload(message.id);
          return { success };
        }
        
        case 'cancelDownload': {
          const success = await cancelDownload(message.id);
          return { success };
        }
        
        case 'confirmDuplicate': {
          // User confirmed duplicate download
          const pending = pendingDuplicates.get(message.id);
          console.log('[Surge] confirmDuplicate called, pending:', pending ? 'found' : 'NOT FOUND', 'id:', message.id);
          if (pending) {
            pendingDuplicates.delete(message.id);
            
            // Mark as seen and proceed with download
            markDownloadSeen(pending.url);
            
            console.log('[Surge] Sending confirmed duplicate to Surge:', pending.url);
            const result = await sendToSurge(
              pending.url,
              pending.filename,
              pending.directory
            );
            console.log('[Surge] sendToSurge result:', result);
            
            if (result.success) {
              browser.notifications.create({
                type: 'basic',
                iconUrl: 'icons/icon48.png',
                title: 'Surge',
                message: `Download started: ${pending.filename || pending.url.split('/').pop()}`,
              });
            }
            
            return { success: result.success };
          } else {
            return { success: false, error: 'Pending download not found' };
          }
        }
        
        case 'skipDuplicate': {
          // User skipped duplicate download
          const pending = pendingDuplicates.get(message.id);
          if (pending) {
            pendingDuplicates.delete(message.id);
            console.log('[Surge] User skipped duplicate download:', pending.url);
          }
          return { success: true };
        }
        
        default:
          return { error: 'Unknown message type' };
      }
    } catch (error) {
      console.error('[Surge] Message handler error:', error);
      return { error: error.message };
    }
  })();
});

// === Initialization ===

async function initialize() {
  console.log('[Surge] Extension initializing...');
  await markExistingDownloads();
  await checkSurgeHealth();
  console.log('[Surge] Extension loaded');
}

initialize();
