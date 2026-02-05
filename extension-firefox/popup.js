// Surge Extension - Popup Script (Firefox)
// Handles UI rendering and communication with background script
// Also supports standalone testing via direct HTTP calls

const SURGE_API_BASE = 'http://127.0.0.1:8080';

// === State ===
let downloads = new Map();
let serverConnected = false;
let pollInterval = null;

// Detect if running in extension context
const isExtensionContext = typeof browser !== 'undefined' && browser.runtime && browser.runtime.sendMessage;

// === DOM Elements ===
const downloadsList = document.getElementById('downloadsList');
const emptyState = document.getElementById('emptyState');
const downloadCount = document.getElementById('downloadCount');
const statusDot = document.getElementById('statusDot');
const statusText = document.getElementById('statusText');
const serverStatus = document.getElementById('serverStatus');
const interceptToggle = document.getElementById('interceptToggle');

// === API Wrapper (works in extension and standalone modes) ===

async function apiCall(action, params = {}) {
  if (isExtensionContext) {
    // Extension mode: use background script
    return browser.runtime.sendMessage({ type: action, ...params });
  } else {
    // Standalone mode: direct HTTP calls
    try {
      switch (action) {
        case 'getDownloads': {
          const response = await fetch(`${SURGE_API_BASE}/list`);
          if (response.ok) {
            const downloads = await response.json();
            return { connected: true, downloads };
          }
          return { connected: false, downloads: [] };
        }
        case 'getStatus':
          return { enabled: true };
        case 'pauseDownload': {
          const response = await fetch(`${SURGE_API_BASE}/pause?id=${params.id}`, { method: 'POST' });
          return { success: response.ok };
        }
        case 'resumeDownload': {
          const response = await fetch(`${SURGE_API_BASE}/resume?id=${params.id}`, { method: 'POST' });
          return { success: response.ok };
        }
        case 'cancelDownload': {
          const response = await fetch(`${SURGE_API_BASE}/delete?id=${params.id}`, { method: 'DELETE' });
          return { success: response.ok };
        }
        default:
          return {};
      }
    } catch (error) {
      console.error('[Surge Popup] API call failed:', error);
      if (action === 'getDownloads') {
        return { connected: false, downloads: [] };
      }
      return { success: false, error: error.message };
    }
  }
}

// === Rendering ===

function renderDownloads() {
  const activeDownloads = [...downloads.values()].filter(
    d => d.status !== 'completed' || Date.now() - (d.completedAt || 0) < 30000
  );

  if (activeDownloads.length === 0) {
    emptyState.classList.remove('hidden');
    downloadCount.textContent = '0';
    const items = downloadsList.querySelectorAll('.download-item');
    items.forEach(item => item.remove());
    return;
  }

  emptyState.classList.add('hidden');
  downloadCount.textContent = activeDownloads.length;

  const statusOrder = { downloading: 0, paused: 1, queued: 2, completed: 3, error: 4 };
  const sorted = activeDownloads.sort((a, b) => {
    const orderA = statusOrder[a.status] ?? 5;
    const orderB = statusOrder[b.status] ?? 5;
    if (orderA !== orderB) return orderA - orderB;
    return (b.addedAt || 0) - (a.addedAt || 0);
  });

  const existingIds = new Set();
  sorted.forEach((dl, index) => {
    existingIds.add(dl.id);
    let item = downloadsList.querySelector(`[data-id="${dl.id}"]`);
    
    if (item) {
      updateDownloadItem(item, dl);
    } else {
      item = createDownloadItem(dl);
      const items = downloadsList.querySelectorAll('.download-item');
      if (index < items.length) {
        items[index].before(item);
      } else {
        downloadsList.insertBefore(item, emptyState);
      }
    }
  });

  const items = downloadsList.querySelectorAll('.download-item');
  items.forEach(item => {
    if (!existingIds.has(item.dataset.id)) {
      item.remove();
    }
  });
}

function createDownloadItem(dl) {
  const item = document.createElement('div');
  item.className = 'download-item';
  item.dataset.id = dl.id;
  updateDownloadItem(item, dl);
  return item;
}

function updateDownloadItem(item, dl) {
  const progress = dl.progress || 0;
  const status = dl.status || 'queued';
  
  item.innerHTML = `
    <div class="download-info">
      <span class="filename" title="${escapeHtml(dl.filename || dl.url)}">${truncate(dl.filename || extractFilename(dl.url), 32)}</span>
      <span class="status-tag ${status}">${status}</span>
    </div>
    <div class="progress-container">
      <div class="progress-bar">
        <div class="progress-fill" style="width: ${progress}%"></div>
      </div>
      <div class="progress-text">
        <span class="size">${formatSize(dl.downloaded)} / ${formatSize(dl.total_size)}</span>
        <span class="progress-percent">${progress.toFixed(1)}%</span>
      </div>
    </div>
    <div class="download-meta">
      <div class="meta-item">
        <span class="meta-icon">⬇</span>
        <span class="speed">${formatSpeed(dl.speed)}</span>
      </div>
      <div class="meta-item">
        <span class="meta-icon">⏱</span>
        <span class="eta">${formatETA(dl.eta)}</span>
      </div>
    </div>
    <div class="download-actions">
      ${status === 'downloading' ? 
        '<button class="action-btn pause" title="Pause">⏸</button>' :
        status === 'paused' || status === 'queued' ? 
        '<button class="action-btn resume" title="Resume">▶</button>' : ''}
      ${status !== 'completed' ? 
        '<button class="action-btn cancel" title="Cancel">✕</button>' : ''}
    </div>
  `;
}

// === Utility Functions ===

function truncate(str, len) {
  if (!str) return 'Unknown';
  return str.length > len ? str.slice(0, len - 3) + '...' : str;
}

function escapeHtml(str) {
  if (!str) return '';
  return str.replace(/[&<>"']/g, char => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  }[char]));
}

function extractFilename(url) {
  if (!url) return 'Unknown';
  try {
    const pathname = new URL(url).pathname;
    const filename = pathname.split('/').pop();
    return decodeURIComponent(filename) || 'Unknown';
  } catch {
    return url.split('/').pop() || 'Unknown';
  }
}

function formatSize(bytes) {
  if (!bytes || bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const value = bytes / Math.pow(1024, i);
  return value.toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
}

function formatSpeed(mbps) {
  if (!mbps || mbps <= 0) return '--';
  if (mbps < 0.01) return (mbps * 1024 * 1024).toFixed(0) + ' B/s';
  if (mbps < 1) return (mbps * 1024).toFixed(1) + ' KB/s';
  return mbps.toFixed(1) + ' MB/s';
}

function formatETA(seconds) {
  if (!seconds || seconds <= 0) return '--:--';
  if (seconds > 86400) return '> 1 day';
  if (seconds > 3600 * 24 * 7) return '> 1 week';
  
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

function updateServerStatus(connected) {
  serverConnected = connected;
  
  if (connected) {
    statusDot.className = 'status-dot online';
    statusText.textContent = 'Connected';
    serverStatus.classList.add('online');
  } else {
    statusDot.className = 'status-dot offline';
    statusText.textContent = 'Offline';
    serverStatus.classList.remove('online');
  }
}

// === Communication with Backend ===

async function fetchDownloads() {
  try {
    const response = await apiCall('getDownloads');
    if (response) {
      updateServerStatus(response.connected);
      if (response.downloads) {
        downloads.clear();
        response.downloads.forEach(dl => downloads.set(dl.id, dl));
      }
      renderDownloads();
    }
  } catch (error) {
    console.error('[Surge Popup] Error fetching downloads:', error);
    updateServerStatus(false);
  }
}

downloadsList.addEventListener('click', async (e) => {
  const btn = e.target.closest('.action-btn');
  if (!btn) return;
  
  const item = btn.closest('.download-item');
  if (!item) return;
  
  const id = item.dataset.id;
  btn.disabled = true;
  btn.style.opacity = '0.5';
  
  try {
    if (btn.classList.contains('pause')) {
      await apiCall('pauseDownload', { id });
    } else if (btn.classList.contains('resume')) {
      await apiCall('resumeDownload', { id });
    } else if (btn.classList.contains('cancel')) {
      await apiCall('cancelDownload', { id });
    }
    await fetchDownloads();
  } catch (error) {
    console.error('[Surge Popup] Action error:', error);
  } finally {
    btn.disabled = false;
    btn.style.opacity = '1';
  }
});

interceptToggle.addEventListener('change', async () => {
  if (isExtensionContext) {
    try {
      await apiCall('setStatus', { enabled: interceptToggle.checked });
    } catch (error) {
      console.error('[Surge Popup] Toggle error:', error);
    }
  }
});

if (isExtensionContext) {
  browser.runtime.onMessage.addListener((message) => {
    if (message.type === 'downloadsUpdate') {
      downloads.clear();
      message.downloads.forEach(dl => downloads.set(dl.id, dl));
      renderDownloads();
    }
    if (message.type === 'serverStatus') {
      updateServerStatus(message.connected);
    }
  });
}

// === Initialization ===

async function init() {
  console.log('[Surge Popup] Initializing...', isExtensionContext ? '(extension mode)' : '(standalone mode)');
  
  try {
    const response = await apiCall('getStatus');
    if (response) {
      interceptToggle.checked = response.enabled !== false;
    }
  } catch (error) {
    console.error('[Surge Popup] Error getting status:', error);
  }
  
  await fetchDownloads();
  pollInterval = setInterval(fetchDownloads, 1000);
}

window.addEventListener('unload', () => {
  if (pollInterval) {
    clearInterval(pollInterval);
  }
});

init();
