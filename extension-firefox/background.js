// Surge Download Manager - Background Script (Firefox)
// Intercepts downloads and sends them to local Surge instance

const DEFAULT_PORT = 8080;
const MAX_PORT_SCAN = 100;
const INTERCEPT_ENABLED_KEY = "interceptEnabled";

// Cache the discovered port
let cachedPort = null;

// Find Surge by scanning ports
async function findSurgePort() {
    // Try cached port first
    if (cachedPort) {
        try {
            const controller = new AbortController();
            const timeoutId = setTimeout(() => controller.abort(), 500);
            const response = await fetch(`http://127.0.0.1:${cachedPort}/health`, {
                method: "GET",
                signal: controller.signal,
            });
            clearTimeout(timeoutId);
            if (response.ok) return cachedPort;
        } catch { }
    }

    // Scan for available port
    for (let port = DEFAULT_PORT; port < DEFAULT_PORT + MAX_PORT_SCAN; port++) {
        try {
            const controller = new AbortController();
            const timeoutId = setTimeout(() => controller.abort(), 300);
            const response = await fetch(`http://127.0.0.1:${port}/health`, {
                method: "GET",
                signal: controller.signal,
            });
            clearTimeout(timeoutId);
            if (response.ok) {
                cachedPort = port;
                console.log(`[Surge] Found server on port ${port}`);
                return port;
            }
        } catch { }
    }
    return null;
}

// Check if Surge is running
async function checkSurgeHealth() {
    const port = await findSurgePort();
    return port !== null;
}

// Send download request to Surge
async function sendToSurge(url, filename) {
    const port = await findSurgePort();
    if (!port) {
        console.error("[Surge] No server found");
        return false;
    }

    try {
        const response = await fetch(`http://127.0.0.1:${port}/download`, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
            },
            body: JSON.stringify({
                url: url,
                filename: filename || "",
                path: "",
            }),
        });

        if (response.ok) {
            const data = await response.json();
            console.log("[Surge] Download queued:", data);
            return true;
        } else {
            console.error("[Surge] Failed to queue download:", response.status);
            return false;
        }
    } catch (error) {
        console.error("[Surge] Error sending to Surge:", error);
        return false;
    }
}

// Check if interception is enabled
async function isInterceptEnabled() {
    const result = await browser.storage.local.get(INTERCEPT_ENABLED_KEY);
    // Default to enabled
    return result[INTERCEPT_ENABLED_KEY] !== false;
}

// Listen for downloads
browser.downloads.onCreated.addListener(async (downloadItem) => {
    console.log("[Surge] Download detected:", downloadItem.url);

    // Check if interception is enabled
    const enabled = await isInterceptEnabled();
    if (!enabled) {
        console.log("[Surge] Interception disabled, using browser download");
        return;
    }

    // Skip blob URLs and data URLs
    if (
        downloadItem.url.startsWith("blob:") ||
        downloadItem.url.startsWith("data:")
    ) {
        console.log("[Surge] Skipping blob/data URL");
        return;
    }

    // Check if Surge is running
    const surgeRunning = await checkSurgeHealth();
    if (!surgeRunning) {
        console.log("[Surge] Server not running, using browser download");
        return;
    }

    // Cancel browser download and send to Surge
    try {
        browser.downloads.cancel(downloadItem.id);
        browser.downloads.erase({ id: downloadItem.id });

        // Firefox sends absolute path in filename, extract just the basename
        const filenameOnly = downloadItem.filename
            ? downloadItem.filename.split(/[/\\]/).pop()
            : "";

        const success = await sendToSurge(
            downloadItem.url,
            filenameOnly
        );

        if (success) {
            browser.notifications.create({
                type: "basic",
                iconUrl: "icons/icon48.png",
                title: "Surge",
                message: `Download sent to Surge: ${filenameOnly || downloadItem.url.split("/").pop()}`,
            });
        }
    } catch (error) {
        console.error("[Surge] Failed to intercept download:", error);
    }
});

// Handle messages from popup
browser.runtime.onMessage.addListener((message, sender, sendResponse) => {
    if (message.type === "checkHealth") {
        checkSurgeHealth().then((healthy) => {
            sendResponse({ healthy });
        });
        return true; // Keep channel open for async response
    }

    if (message.type === "getStatus") {
        isInterceptEnabled().then((enabled) => {
            sendResponse({ enabled });
        });
        return true;
    }

    if (message.type === "setStatus") {
        browser.storage.local.set({ [INTERCEPT_ENABLED_KEY]: message.enabled });
        sendResponse({ success: true });
        return true;
    }
});

console.log("[Surge] Extension loaded");
