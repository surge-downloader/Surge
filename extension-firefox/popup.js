// Surge Extension - Popup Script
// Handles UI rendering and communication with background service worker
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

// Duplicate modal elements
const duplicateModal = document.getElementById('duplicateModal');
const duplicateFilename = document.getElementById('duplicateFilename');
const duplicateConfirm = document.getElementById('duplicateConfirm');
const duplicateSkip = document.getElementById('duplicateSkip');

// Pending duplicate state
let pendingDuplicateId = null;
let duplicateTimeout = null;

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
          return { enabled: true }; // Always enabled in standalone
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
    // Clear any existing download items
    const items = downloadsList.querySelectorAll('.download-item');
    items.forEach(item => item.remove());
    return;
  }

  emptyState.classList.add('hidden');
  downloadCount.textContent = activeDownloads.length;

  // Sort: downloading first, then paused, then queued, then completed
  const statusOrder = { downloading: 0, paused: 1, queued: 2, completed: 3, error: 4 };
  const sorted = activeDownloads.sort((a, b) => {
    const orderA = statusOrder[a.status] ?? 5;
    const orderB = statusOrder[b.status] ?? 5;
    if (orderA !== orderB) return orderA - orderB;
    return (b.addedAt || 0) - (a.addedAt || 0);
  });

  // Update or create items
  const existingIds = new Set();
  sorted.forEach((dl, index) => {
    existingIds.add(dl.id);
    let item = downloadsList.querySelector(`[data-id="${dl.id}"]`);
    
    if (item) {
      updateDownloadItem(item, dl);
    } else {
      item = createDownloadItem(dl);
      // Insert at correct position
      const items = downloadsList.querySelectorAll('.download-item');
      if (index < items.length) {
        items[index].before(item);
      } else {
        downloadsList.insertBefore(item, emptyState);
      }
    }
  });

  // Remove stale items
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
  
  // Initial structure
  item.innerHTML = `
    <div class="download-header" data-toggle>
      <div class="download-main">
        <span class="filename" title=""></span>
        <div class="download-quick-stats">
          <span class="speed-compact"></span>
          <span class="eta-compact"></span>
          <span class="progress-compact"></span>
        </div>
      </div>
      <div class="download-header-right">
        <span class="status-tag"></span>
        <span class="expand-icon">▶</span>
      </div>
    </div>
    <div class="download-details">
      <div class="progress-container">
        <div class="progress-bar">
          <div class="progress-fill" style="width: 0%"></div>
        </div>
        <div class="progress-text">
          <span class="size"></span>
          <span class="progress-percent"></span>
        </div>
      </div>
      <div class="download-actions">
        <!-- Buttons injected dynamically -->
      </div>
    </div>
  `;
  
  updateDownloadItem(item, dl);
  return item;
}

function updateDownloadItem(item, dl) {
  const progress = dl.progress || 0;
  const status = dl.status || 'queued';
  const isExpanded = item.classList.contains('expanded');

  // 1. Update text content only if changed (optional optimization, but simple assignment is fast)
  const els = {
    filename: item.querySelector('.filename'),
    speed: item.querySelector('.speed-compact'),
    eta: item.querySelector('.eta-compact'),
    progCompact: item.querySelector('.progress-compact'),
    status: item.querySelector('.status-tag'),
    icon: item.querySelector('.expand-icon'),
    fill: item.querySelector('.progress-fill'),
    size: item.querySelector('.size'),
    progPercent: item.querySelector('.progress-percent'),
    actions: item.querySelector('.download-actions')
  };

  // Safe checks in case DOM is malformed
  if (!els.filename) return; 

  // Filename
  const fname = dl.filename || dl.url;
  const shortName = truncate(dl.filename || extractFilename(dl.url), 28);
  if (els.filename.textContent !== shortName) {
    els.filename.textContent = shortName;
    els.filename.title = escapeHtml(fname);
  }

  // Stats
  els.speed.textContent = formatSpeed(dl.speed);
  els.eta.textContent = formatETA(dl.eta);
  els.progCompact.textContent = progress.toFixed(0) + '%';
  
  // Status
  els.status.className = `status-tag ${status}`;
  els.status.textContent = status;
  
  // Icon
  els.icon.textContent = isExpanded ? '▼' : '▶';

  // Progress Bar
  els.fill.style.width = `${progress}%`;
  
  // Details
  els.size.textContent = `${formatSize(dl.downloaded)} / ${formatSize(dl.total_size)}`;
  els.progPercent.textContent = progress.toFixed(1) + '%';

  // Actions - Only update if status implies different buttons
  // To avoid replacing active buttons (which loses 'disabled' state), we check if the current buttons match the desired state.
  // A simple heuristic: check the first button's class.
  let desiredButtons = '';
  if (status === 'downloading') {
    desiredButtons = '<button class="action-btn pause" title="Pause">⏸</button>';
  } else if (status === 'paused' || status === 'queued') {
    desiredButtons = '<button class="action-btn resume" title="Resume">▶</button>';
  }
  
  if (status !== 'completed') {
    desiredButtons += '<button class="action-btn cancel" title="Cancel">✕</button>';
  }

  const currentFirstBtn = els.actions.querySelector('.action-btn:first-child');
  let currentType = 'none';
  if (currentFirstBtn) {
    if (currentFirstBtn.classList.contains('pause')) currentType = 'pause';
    else if (currentFirstBtn.classList.contains('resume')) currentType = 'resume';
  }

  let desiredType = 'none';
  if (status === 'downloading') desiredType = 'pause';
  else if (status === 'paused' || status === 'queued') desiredType = 'resume';

  const hasCancel = !!els.actions.querySelector('.cancel');
  const needsCancel = status !== 'completed';

  if (currentType !== desiredType || hasCancel !== needsCancel) {
     els.actions.innerHTML = desiredButtons;
  }
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

// Handle toggle expand/collapse
downloadsList.addEventListener('click', (e) => {
  const toggleHeader = e.target.closest('[data-toggle]');
  if (toggleHeader && !e.target.closest('.action-btn')) {
    const item = toggleHeader.closest('.download-item');
    if (item) {
      item.classList.toggle('expanded');
      const expandIcon = item.querySelector('.expand-icon');
      if (expandIcon) {
        expandIcon.textContent = item.classList.contains('expanded') ? '▼' : '▶';
      }
    }
  }
});

// Handle action button clicks
downloadsList.addEventListener('click', async (e) => {
  const btn = e.target.closest('.action-btn');
  if (!btn) return;
  
  const item = btn.closest('.download-item');
  if (!item) return;
  
  const id = item.dataset.id;
  
  // Disable button temporarily
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
    // Refresh immediately after action
    await fetchDownloads();
  } catch (error) {
    console.error('[Surge Popup] Action error:', error);
  } finally {
    btn.disabled = false;
    btn.style.opacity = '1';
  }
});

// Handle toggle change
interceptToggle.addEventListener('change', async () => {
  if (isExtensionContext) {
    try {
      await apiCall('setStatus', { enabled: interceptToggle.checked });
    } catch (error) {
      console.error('[Surge Popup] Toggle error:', error);
    }
  }
});

// === Duplicate Download Modal ===

function showDuplicateModal(id, filename) {
  pendingDuplicateId = id;
  duplicateFilename.textContent = filename || 'Unknown file';
  duplicateModal.classList.remove('hidden');
  
  // Auto-dismiss after 30 seconds
  if (duplicateTimeout) {
    clearTimeout(duplicateTimeout);
  }
  duplicateTimeout = setTimeout(() => {
    hideDuplicateModal();
    // Send skip response on timeout
    if (isExtensionContext && pendingDuplicateId) {
      apiCall('skipDuplicate', { id: pendingDuplicateId });
    }
  }, 30000);
}

function hideDuplicateModal() {
  duplicateModal.classList.add('hidden');
  pendingDuplicateId = null;
  if (duplicateTimeout) {
    clearTimeout(duplicateTimeout);
    duplicateTimeout = null;
  }
}

// Duplicate modal button handlers
duplicateConfirm.addEventListener('click', async () => {
  if (!pendingDuplicateId) return;
  
  const id = pendingDuplicateId;
  hideDuplicateModal();
  
  if (isExtensionContext) {
    try {
      await apiCall('confirmDuplicate', { id });
    } catch (error) {
      console.error('[Surge Popup] Confirm duplicate error:', error);
    }
  }
});

duplicateSkip.addEventListener('click', async () => {
  if (!pendingDuplicateId) return;
  
  const id = pendingDuplicateId;
  hideDuplicateModal();
  
  if (isExtensionContext) {
    try {
      await apiCall('skipDuplicate', { id });
    } catch (error) {
      console.error('[Surge Popup] Skip duplicate error:', error);
    }
  }
});

// Close modal on escape key
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && pendingDuplicateId) {
    duplicateSkip.click();
  }
});

// Listen for messages from background (extension mode only)
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
    if (message.type === 'promptDuplicate') {
      showDuplicateModal(message.id, message.filename);
    }
  });
}

// === Initialization ===

async function init() {
  console.log('[Surge Popup] Initializing...', isExtensionContext ? '(extension mode)' : '(standalone mode)');
  
  // Get current toggle state
  try {
    const response = await apiCall('getStatus');
    if (response) {
      interceptToggle.checked = response.enabled !== false;
    }
  } catch (error) {
    console.error('[Surge Popup] Error getting status:', error);
  }

  // Check for pending duplicates
  if (isExtensionContext) {
    try {
      const response = await apiCall('getPendingDuplicates');
      if (response && response.duplicates && response.duplicates.length > 0) {
        // Show the first one
        const dup = response.duplicates[0];
        showDuplicateModal(dup.id, dup.filename);
      }
    } catch (error) {
      console.error('[Surge Popup] Error checking duplicates:', error);
    }
  }
  
  // Initial fetch
  await fetchDownloads();
  
  // Poll for updates every 1 second
  pollInterval = setInterval(fetchDownloads, 1000);
}

// Cleanup when popup closes
window.addEventListener('unload', () => {
  if (pollInterval) {
    clearInterval(pollInterval);
  }
});

// Start
init();
