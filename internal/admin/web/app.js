// ===== App State =====
const state = {
    currentTab: 'dashboard',
    logs: [],
    files: [],
    identity: {},
    autoRefreshInterval: null,
    startTime: Date.now(),
    sidebarOpen: false,
    // Files 查询与分页状态
    filesQuery: { keyword: '', type: '', from: '', to: '' },
    filesPage: 1,
    filesPageSize: 20,
    filesTotal: 0,
    grandTotal: 0,
    grandSize: 0
};

// ===== Mobile Sidebar =====
function toggleSidebar() {
    const sidebar = document.getElementById('sidebar');
    const overlay = document.getElementById('sidebarOverlay');
    state.sidebarOpen = !state.sidebarOpen;

    if (state.sidebarOpen) {
        sidebar.classList.add('open');
        overlay.classList.add('active');
    } else {
        sidebar.classList.remove('open');
        overlay.classList.remove('active');
    }
}

function closeSidebar() {
    const sidebar = document.getElementById('sidebar');
    const overlay = document.getElementById('sidebarOverlay');
    state.sidebarOpen = false;
    sidebar.classList.remove('open');
    overlay.classList.remove('active');
}

// ===== Navigation =====
function navigateToTab(tabName) {
    // Update tab content visibility
    document.querySelectorAll('.tab-content').forEach(tab => {
        tab.classList.remove('active');
    });
    document.getElementById(`tab-${tabName}`).classList.add('active');
    
    // Update nav items
    document.querySelectorAll('.nav-item').forEach(item => {
        item.classList.remove('active');
    });
    document.querySelector(`.nav-item[data-tab="${tabName}"]`).classList.add('active');
    
    // Update page title
    const titles = {
        dashboard: 'Dashboard',
        files: 'Received Files',
        logs: 'Transfer Logs',
        settings: 'Settings'
    };
    document.getElementById('pageTitle').textContent = titles[tabName];
    
    state.currentTab = tabName;
    closeSidebar();
    
    // Load data for the tab
    if (tabName === 'dashboard') {
        loadDashboard();
    } else if (tabName === 'files') {
        loadFiles();
    } else if (tabName === 'logs') {
        loadLogs();
    }
}

// ===== Data Loading =====
async function loadConfig() {
    try {
        const res = await fetch('/api/config');
        const data = await res.json();
        document.getElementById('dirInput').value = data.receiveDir || '';
        const sysDir = document.getElementById('sysReceiveDir');
        if (sysDir) {
            sysDir.textContent = data.receiveDir || 'Not set';
        }
    } catch(e) {
        console.error('Failed to load config:', e);
        const sysDir = document.getElementById('sysReceiveDir');
        if (sysDir) {
            sysDir.textContent = 'Error loading';
        }
    }
}

async function loadIdentity() {
    try {
        const res = await fetch('/api/identity');
        const data = await res.json();
        state.identity = data;
        
        document.getElementById('inputAlias').value = data.alias || '';
        document.getElementById('inputModel').value = data.deviceModel || '';
        document.getElementById('inputType').value = data.deviceType || 'server';
    } catch(e) {
        console.error('Failed to load identity:', e);
    }
}

async function saveIdentity() {
    const alias = document.getElementById('inputAlias').value;
    const model = document.getElementById('inputModel').value;
    const type = document.getElementById('inputType').value;
    
    try {
        const btn = document.querySelector('#tab-settings .btn-primary');
        const originalText = btn.innerHTML;
        btn.innerHTML = '<i class="fas fa-spinner fa-spin"></i> Saving...';
        btn.disabled = true;
        
        await fetch('/api/identity', { 
            method: 'POST', 
            body: JSON.stringify({ 
                alias, 
                deviceModel: model, 
                deviceType: type 
            }) 
        });
        
        btn.innerHTML = '<i class="fas fa-check"></i> Saved!';
        showToast('Identity updated successfully', 'success');
        
        setTimeout(() => {
            btn.innerHTML = originalText;
            btn.disabled = false;
        }, 1500);
    } catch(e) {
        showToast('Failed to update identity', 'error');
        console.error(e);
    }
}

async function loadFiles() {
    try {
        const params = new URLSearchParams();
        if (state.filesQuery.keyword) params.set('keyword', state.filesQuery.keyword);
        if (state.filesQuery.type) params.set('type', state.filesQuery.type);
        if (state.filesQuery.from) params.set('from', state.filesQuery.from);
        if (state.filesQuery.to) params.set('to', state.filesQuery.to);
        params.set('page', state.filesPage);
        params.set('pageSize', state.filesPageSize);

        const res = await fetch('/api/files?' + params.toString());
        const data = await res.json();

        state.files = data.items || [];
        state.filesTotal = data.total || 0;
        state.filesPage = data.page || 1;
        state.filesPageSize = data.pageSize || state.filesPageSize;
        state.grandTotal = data.grandTotal || 0;
        state.grandSize = data.grandSize || 0;

        updateTypeOptions(data.availableTypes || []);
        renderFilesTable();
        renderFilesPagination();
        updateStats();
    } catch(e) {
        console.error('Failed to load files:', e);
        showToast('Failed to load files', 'error');
    }
}

function renderFilesTable() {
    const tbody = document.getElementById('filesBody');

    if (state.files.length === 0) {
        tbody.innerHTML = `
            <tr>
                <td colspan="4" class="empty-state">
                    <i class="fas fa-inbox fa-3x"></i>
                    <p>No files match your query</p>
                </td>
            </tr>
        `;
        return;
    }

    tbody.innerHTML = state.files.map(f => `
        <tr>
            <td>
                <i class="fas fa-file" style="color: var(--primary); margin-right: 0.5rem;"></i>
                ${escapeHtml(f.name)}
            </td>
            <td>${formatSize(f.size)}</td>
            <td>${formatDateTime(f.modTime)}</td>
            <td>
                <a href="${f.url}" class="file-link" target="_blank">
                    <i class="fas fa-download"></i> Download
                </a>
            </td>
        </tr>
    `).join('');
}

function renderFilesPagination() {
    const pages = Math.max(1, Math.ceil(state.filesTotal / state.filesPageSize));
    if (state.filesPage > pages) state.filesPage = pages;

    const totalEl = document.getElementById('filesTotal');
    if (totalEl) totalEl.textContent = `${state.filesTotal} file(s)`;

    const infoEl = document.getElementById('filesPageInfo');
    if (infoEl) infoEl.textContent = `Page ${state.filesPage} / ${pages}`;

    const prev = document.getElementById('fPrev');
    const next = document.getElementById('fNext');
    if (prev) prev.disabled = state.filesPage <= 1;
    if (next) next.disabled = state.filesPage >= pages;
}

function updateTypeOptions(types) {
    const sel = document.getElementById('fType');
    if (!sel) return;
    const current = sel.value;
    sel.innerHTML = '<option value="">All types</option>' +
        types.map(t => `<option value="${escapeHtml(t)}">.${escapeHtml(t)}</option>`).join('');
    // 保留当前选中项 (若仍存在)
    if (current && types.includes(current)) {
        sel.value = current;
    }
}

function applyFileFilter() {
    state.filesQuery = {
        keyword: document.getElementById('fKeyword').value.trim(),
        type: document.getElementById('fType').value,
        from: document.getElementById('fFrom').value,
        to: document.getElementById('fTo').value
    };
    state.filesPage = 1;
    loadFiles();
}

function resetFileFilter() {
    document.getElementById('fKeyword').value = '';
    document.getElementById('fType').value = '';
    document.getElementById('fFrom').value = '';
    document.getElementById('fTo').value = '';
    state.filesQuery = { keyword: '', type: '', from: '', to: '' };
    state.filesPage = 1;
    loadFiles();
}

function filesPrevPage() {
    if (state.filesPage > 1) {
        state.filesPage--;
        loadFiles();
    }
}

function filesNextPage() {
    const pages = Math.max(1, Math.ceil(state.filesTotal / state.filesPageSize));
    if (state.filesPage < pages) {
        state.filesPage++;
        loadFiles();
    }
}

function changeFilesPageSize() {
    state.filesPageSize = parseInt(document.getElementById('fPageSize').value, 10) || 20;
    state.filesPage = 1;
    loadFiles();
}

async function loadLogs() {
    try {
        const res = await fetch('/api/logs');
        const logs = await res.json();
        state.logs = logs;
        
        const tbody = document.getElementById('logsBody');
        
        if (logs.length === 0) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="5" class="empty-state">
                        <i class="fas fa-clipboard fa-3x"></i>
                        <p>No transfer logs yet</p>
                    </td>
                </tr>
            `;
            return;
        }
        
        tbody.innerHTML = logs.map(l => `
            <tr>
                <td>
                    <i class="fas fa-clock" style="color: var(--text-muted); margin-right: 0.5rem;"></i>
                    ${formatDateTime(l.time)}
                </td>
                <td>
                    <i class="fas fa-file" style="color: var(--primary); margin-right: 0.5rem;"></i>
                    ${escapeHtml(l.filename)}
                </td>
                <td>${formatSize(l.size)}</td>
                <td>
                    <i class="fas fa-user" style="color: var(--text-muted); margin-right: 0.5rem;"></i>
                    ${escapeHtml(l.sender.split(':')[0])}
                </td>
                <td>
                    <span class="status-badge ${l.status === 'Success' ? 'success' : 'danger'}">
                        <i class="fas fa-${l.status === 'Success' ? 'check' : 'times'}"></i>
                        ${l.status}
                    </span>
                </td>
            </tr>
        `).join('');
        
        // Update dashboard
        loadDashboard();
    } catch(e) {
        console.error('Failed to load logs:', e);
        showToast('Failed to load logs', 'error');
    }
}

async function loadDashboard() {
    // Load recent activity
    const recentDiv = document.getElementById('recentActivity');
    
    if (state.logs.length === 0) {
        recentDiv.innerHTML = `
            <div class="empty-state">
                <i class="fas fa-history fa-2x"></i>
                <p>No recent activity</p>
            </div>
        `;
        return;
    }
    
    const recentLogs = state.logs.slice(0, 5);
    recentDiv.innerHTML = recentLogs.map(l => `
        <div class="activity-item">
            <div class="activity-icon ${l.status === 'Success' ? 'success' : 'danger'}">
                <i class="fas fa-${l.status === 'Success' ? 'check' : 'times'}"></i>
            </div>
            <div class="activity-info">
                <h4>${escapeHtml(l.filename)}</h4>
                <p>From: ${escapeHtml(l.sender.split(':')[0])}</p>
            </div>
            <div class="activity-meta">
                <span>${formatSize(l.size)}</span>
                <span>${formatDateTime(l.time)}</span>
            </div>
        </div>
    `).join('');
    
    updateStats();
}

function updateStats() {
    const successCount = state.logs.filter(l => l.status === 'Success').length;
    const failCount = state.logs.filter(l => l.status !== 'Success').length;

    document.getElementById('successCount').textContent = successCount;
    document.getElementById('failCount').textContent = failCount;
    // 文件总数/总大小取全目录统计，不受过滤和分页影响
    document.getElementById('totalFiles').textContent = state.grandTotal;
    document.getElementById('totalSize').textContent = formatSize(state.grandSize);
}

async function saveConfig() {
    const dir = document.getElementById('dirInput').value.trim();

    if (!dir) {
        showToast('Please enter a directory path', 'error');
        return;
    }

    try {
        const res = await fetch('/api/config', {
            method: 'POST',
            body: JSON.stringify({ receiveDir: dir })
        });

        if (!res.ok) {
            const msg = await res.text().catch(() => '');
            showToast('Failed to update directory: ' + (msg || res.statusText), 'error');
            return;
        }

        showToast('Directory updated successfully', 'success');
    } catch(e) {
        showToast('Failed to update directory', 'error');
        console.error(e);
    }
}

async function clearLogs() {
    if (!confirm('Are you sure you want to clear all logs?')) {
        return;
    }
    
    try {
        await fetch('/api/logs', { method: 'DELETE' });
        state.logs = [];
        loadLogs();
        showToast('Logs cleared successfully', 'success');
    } catch(e) {
        showToast('Failed to clear logs', 'error');
        console.error(e);
    }
}

async function refreshData() {
    const btn = document.querySelector('.icon-btn[onclick="refreshData()"] i');
    btn.classList.add('fa-spin');

    await Promise.all([
        loadConfig(),
        loadFiles(),
        loadLogs(),
        loadIdentity()
    ]);

    setTimeout(() => {
        btn.classList.remove('fa-spin');
    }, 500);

    showToast('Data refreshed', 'success');
}

// ===== Theme Toggle =====
function toggleTheme() {
    document.body.classList.toggle('light-theme');
    const isLight = document.body.classList.contains('light-theme');
    localStorage.setItem('theme', isLight ? 'light' : 'dark');
    
    const icon = document.querySelector('.icon-btn[onclick="toggleTheme()"] i');
    icon.className = isLight ? 'fas fa-sun' : 'fas fa-moon';
}

// ===== Toast Notifications =====
function showToast(message, type = 'success') {
    const toast = document.getElementById('toast');
    const toastMsg = document.getElementById('toastMessage');
    
    toast.className = `toast ${type}`;
    toastMsg.textContent = message;
    
    const icon = toast.querySelector('i');
    icon.className = type === 'success' ? 'fas fa-check-circle' : 'fas fa-exclamation-circle';
    
    toast.classList.remove('hidden');
    
    setTimeout(() => {
        toast.classList.add('hidden');
    }, 3000);
}

// ===== Utility Functions =====
function formatSize(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

// formatDateTime 将服务端返回的 RFC3339 时间戳按查看者本地时区格式化
// 无法解析时原样返回 (兼容旧格式)
function formatDateTime(s) {
    if (!s) return '';
    const d = new Date(s);
    if (isNaN(d.getTime())) return s;
    const pad = n => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} `
         + `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function updateUptime() {
    const elapsed = Date.now() - state.startTime;
    const hours = Math.floor(elapsed / (1000 * 60 * 60));
    const minutes = Math.floor((elapsed % (1000 * 60 * 60)) / (1000 * 60));
    const seconds = Math.floor((elapsed % (1000 * 60)) / 1000);
    
    document.getElementById('uptime').textContent = 
        `Uptime: ${hours}h ${minutes}m ${seconds}s`;
}

// ===== Event Listeners =====
document.addEventListener('DOMContentLoaded', () => {
    // Setup navigation
    document.querySelectorAll('.nav-item').forEach(item => {
        item.addEventListener('click', (e) => {
            e.preventDefault();
            const tab = item.dataset.tab;
            navigateToTab(tab);
        });
    });

    // Mobile menu toggle
    document.getElementById('menuToggle').addEventListener('click', toggleSidebar);
    document.getElementById('sidebarOverlay').addEventListener('click', closeSidebar);

    // Load initial data
    loadConfig();
    loadIdentity();
    loadFiles();
    loadLogs();
    
    // Auto-refresh logs every 5 seconds
    state.autoRefreshInterval = setInterval(() => {
        if (state.currentTab === 'logs' || state.currentTab === 'dashboard') {
            loadLogs();
        }
    }, 5000);
    
    // Update uptime every second
    setInterval(updateUptime, 1000);
    
    // Load saved theme
    const savedTheme = localStorage.getItem('theme');
    if (savedTheme === 'light') {
        document.body.classList.add('light-theme');
        const icon = document.querySelector('.icon-btn[onclick="toggleTheme()"] i');
        if (icon) icon.className = 'fas fa-sun';
    }
});

// ===== Make functions available globally =====
window.navigateToTab = navigateToTab;
window.saveIdentity = saveIdentity;
window.saveConfig = saveConfig;
window.clearLogs = clearLogs;
window.loadFiles = loadFiles;
window.loadLogs = loadLogs;
window.refreshData = refreshData;
window.toggleTheme = toggleTheme;
window.toggleSidebar = toggleSidebar;
window.closeSidebar = closeSidebar;
window.applyFileFilter = applyFileFilter;
window.resetFileFilter = resetFileFilter;
window.filesPrevPage = filesPrevPage;
window.filesNextPage = filesNextPage;
window.changeFilesPageSize = changeFilesPageSize;
