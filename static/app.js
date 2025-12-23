document.addEventListener('DOMContentLoaded', () => {
    fetchConfig();
    fetchHistory();

    // Event Listeners
    document.getElementById('add-target-btn').addEventListener('click', () => openTargetModal());
    document.querySelector('.close-modal').addEventListener('click', closeModal);
    document.querySelector('#target-modal .secondary-btn').addEventListener('click', closeModal);

    // Fix: Moved form submit listener inside and removed ReferenceError
    document.getElementById('target-form').addEventListener('submit', handleFormSubmit);

    document.getElementById('refresh-history-btn').addEventListener('click', () => fetchHistory(1));
    document.getElementById('prev-page-btn').addEventListener('click', () => changePage(-1));
    document.getElementById('next-page-btn').addEventListener('click', () => changePage(1));

    // Global Config Save Listener
    const globalSaveBtn = document.getElementById('save-global-btn');
    if (globalSaveBtn) {
        globalSaveBtn.addEventListener('click', async () => {
            const webhook = document.getElementById('global-webhook').value;
            const interval = parseInt(document.getElementById('global-interval').value);

            if (isNaN(interval) || interval < 10) {
                alert('检查间隔必须大于等于 10 秒');
                return;
            }

            currentConfig.webhook_url = webhook;
            currentConfig.check_interval = interval;

            await saveConfig();
            alert('通用配置已保存');
        });
    }

    // Close modal on outside click
    document.getElementById('target-modal').addEventListener('click', (e) => {
        if (e.target.id === 'target-modal') closeModal();
    });

    // Limit Selector Listener
    document.getElementById('limit-selector').addEventListener('change', (e) => {
        historyState.limit = parseInt(e.target.value);
        historyState.page = 1;
        fetchHistory(1);
    });

    // Filter and Sort Listeners
    document.getElementById('source-filter').addEventListener('change', (e) => {
        historyState.source = e.target.value;
        historyState.page = 1; // Reset to first page
        fetchHistory(1);
    });

    document.querySelectorAll('.sortable').forEach(th => {
        th.addEventListener('click', () => {
            const field = th.dataset.sort;
            if (historyState.sortBy === field) {
                // Toggle order
                historyState.order = historyState.order === 'ASC' ? 'DESC' : 'ASC';
            } else {
                // New field, default to DESC (usually what we want for dates)
                historyState.sortBy = field;
                historyState.order = 'DESC';
            }
            historyState.page = 1;
            fetchHistory(1);
        });
    });
});

function updateSourceFilterOptions() {
    const select = document.getElementById('source-filter');
    const currentVal = select.value;

    // Clear existing options except 'all'
    select.innerHTML = '<option value="all">所有来源</option>';

    // Get unique sources from config targets (as they map to source names usually)
    if (currentConfig.targets) {
        currentConfig.targets.forEach(target => {
            const option = document.createElement('option');
            option.value = target.name;
            option.textContent = target.name;
            select.appendChild(option);
        });
    }

    // Restore selection if possible
    select.value = currentVal;
}

let currentConfig = { targets: [] };
let historyState = {
    page: 1,
    limit: 10,
    total: 0,
    totalPages: 1,
    sortBy: 'publish_date',
    order: 'DESC',
    source: 'all'
};
let editingIndex = -1;

async function fetchConfig() {
    try {
        const response = await fetch('/api/config');
        if (!response.ok) throw new Error('Failed to fetch config');
        currentConfig = await response.json();

        // Populate Global Config inputs
        const webhookInput = document.getElementById('global-webhook');
        const intervalInput = document.getElementById('global-interval');
        if (webhookInput) webhookInput.value = currentConfig.webhook_url || '';
        if (intervalInput) intervalInput.value = currentConfig.check_interval || 3600;

        renderTargets();
        updateSourceFilterOptions(); // Populate filter dropdown
    } catch (err) {
        console.error(err);
        alert('无法加载配置: ' + err.message);
    }
}

async function fetchHistory(page) {
    if (page) historyState.page = page;

    // Ensure source state is synced from DOM
    const select = document.getElementById('source-filter');
    if (select) {
        historyState.source = select.value;
    }

    try {
        const queryParams = new URLSearchParams({
            page: historyState.page,
            limit: historyState.limit,
            sort_by: historyState.sortBy,
            order: historyState.order,
            source: historyState.source
        });

        const response = await fetch(`/api/history?${queryParams.toString()}`);
        if (!response.ok) throw new Error('Failed to fetch history');
        const data = await response.json();

        historyState.page = data.page;
        historyState.total = data.total;
        historyState.totalPages = data.total_pages;

        renderHistory(data.items || []);
        updatePagination();
        updateSortIndicators();
    } catch (err) {
        console.error(err);
    }
}

function updateSortIndicators() {
    document.querySelectorAll('.sortable').forEach(th => {
        const field = th.dataset.sort;
        const indicator = th.querySelector('.sort-indicator');
        if (field === historyState.sortBy) {
            indicator.textContent = historyState.order === 'ASC' ? '▲' : '▼';
            th.style.color = 'var(--primary-color)';
        } else {
            indicator.textContent = '';
            th.style.color = '';
        }
    });
}

function updatePagination() {
    document.getElementById('page-info').textContent = `第 ${historyState.page} / ${historyState.totalPages} 页`;
    document.getElementById('prev-page-btn').disabled = historyState.page <= 1;
    document.getElementById('next-page-btn').disabled = historyState.page >= historyState.totalPages;
}

function changePage(delta) {
    const newPage = historyState.page + delta;
    if (newPage >= 1 && newPage <= historyState.totalPages) {
        fetchHistory(newPage);
    }
}

function renderTargets() {
    const list = document.getElementById('targets-list');
    list.innerHTML = '';

    currentConfig.targets.forEach((target, index) => {
        const card = document.createElement('div');
        card.className = 'target-card';

        const keywordsHtml = target.keywords && target.keywords.length > 0
            ? target.keywords.map(k => `<span class="tag">${k}</span>`).join('')
            : '<span class="tag" style="background: #f1f5f9; font-style: italic;">全部抓取</span>';

        const statusHtml = target.enabled !== false
            ? '<span class="status-badge status-enabled">已启用</span>'
            : '<span class="status-badge status-disabled">已停用</span>';

        const toggleBtnLabel = target.enabled !== false ? '停用' : '启用';

        card.innerHTML = `
            <div class="target-header">
                <div class="target-name">
                    ${target.name}
                    ${statusHtml}
                </div>
                <div class="target-actions-group">
                    <button class="btn icon-btn" onclick="toggleTarget(${index})" title="${toggleBtnLabel}">
                         <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M18.36 6.64a9 9 0 1 1-12.73 0"></path>
                            <line x1="12" y1="2" x2="12" y2="12"></line>
                        </svg>
                    </button>
                    <button class="btn icon-btn" onclick="editTarget(${index})" title="编辑">
                         <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
                            <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
                        </svg>
                    </button>
                    <button class="btn icon-btn danger-btn" onclick="deleteTarget(${index})" title="删除">
                         <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <polyline points="3 6 5 6 21 6"></polyline>
                            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                        </svg>
                    </button>
                </div>
            </div>
            <a href="${target.url}" target="_blank" class="target-url" title="${target.url}">${target.url}</a>
            <div class="keywords-tags">
                ${keywordsHtml}
            </div>
        `;
        list.appendChild(card);
    });
}

async function toggleTarget(index) {
    const target = currentConfig.targets[index];
    target.enabled = target.enabled === false ? true : false;
    await saveConfig();
}

function renderHistory(history) {
    const tbody = document.getElementById('history-body');
    tbody.innerHTML = '';

    if (history.length === 0) {
        tbody.innerHTML = '<tr><td colspan="5" style="text-align: center; color: var(--text-secondary);">暂无历史记录</td></tr>';
        return;
    }

    history.forEach((item, index) => {
        const tr = document.createElement('tr');

        // Calculate sequence number
        const seqNum = (historyState.page - 1) * historyState.limit + index + 1;

        // Format dates
        const fetchedAt = item.fetched_at ? new Date(item.fetched_at).toLocaleString() : '-';
        const publishDate = item.date || '-';

        tr.innerHTML = `
            <td class="date-cell" style="text-align: center;">${seqNum}</td>
            <td class="date-cell">${publishDate}</td>
            <td style="font-weight: 500;">${item.title || '无标题'}</td>
            <td><span class="source-badge">${item.source || '未知'}</span></td>
            <td>
                <a href="${item.url}" target="_blank" class="link-btn">查看</a>
                <button onclick="repushItem(${item.id}, this)" class="btn link-btn" style="color: var(--primary-color); margin-left: 8px;">推送</button>
            </td>
            <td class="date-cell" style="font-size: 0.8rem; text-align: right;">${fetchedAt}</td>
        `;
        tbody.appendChild(tr);
    });
}

async function repushItem(id, btn) {
    if (btn.disabled) return;
    const originalText = btn.textContent;
    btn.disabled = true;
    btn.textContent = '...';

    try {
        const response = await fetch('/api/repush', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: id })
        });

        if (!response.ok) {
            const errText = await response.text();
            throw new Error(errText || 'failed');
        }

        btn.textContent = '已推送';
        setTimeout(() => {
            btn.textContent = originalText;
            btn.disabled = false;
        }, 2000);
    } catch (err) {
        console.error(err);
        alert('推送失败: ' + err.message);
        btn.textContent = originalText;
        btn.disabled = false;
    }
}

async function deleteTarget(index) {
    if (!confirm('确定要删除这个监控目标吗？')) return;

    currentConfig.targets.splice(index, 1);
    await saveConfig();
}

function editTarget(index) {
    openTargetModal(index);
}

async function saveConfig() {
    try {
        const response = await fetch('/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(currentConfig)
        });

        if (!response.ok) throw new Error('Failed to save config');

        await fetchConfig(); // Reload
        // alert('配置已保存'); // Less intrusive
    } catch (err) {
        console.error(err);
        alert('保存失败: ' + err.message);
    }
}

// Modal Logic
function openTargetModal(index = -1) {
    const form = document.getElementById('target-form');
    form.reset();
    editingIndex = index;

    if (index >= 0) {
        document.getElementById('modal-title').textContent = '编辑监控目标';
        const target = currentConfig.targets[index];
        document.getElementById('target-name').value = target.name;
        document.getElementById('target-url').value = target.url;
        document.getElementById('target-keywords').value = target.keywords ? target.keywords.join(', ') : '';
        document.getElementById('target-enabled').checked = target.enabled !== false;
    } else {
        document.getElementById('modal-title').textContent = '添加监控目标';
        document.getElementById('target-enabled').checked = true;
    }

    document.getElementById('target-modal').classList.remove('hidden');
}

function closeModal() {
    document.getElementById('target-modal').classList.add('hidden');
    editingIndex = -1;
}

// Define handleFormSubmit and use it
async function handleFormSubmit(e) {
    e.preventDefault();

    const name = document.getElementById('target-name').value;
    const url = document.getElementById('target-url').value;
    const keywordsStr = document.getElementById('target-keywords').value;
    const enabled = document.getElementById('target-enabled').checked;

    const keywords = keywordsStr.split(/[,，]/).map(k => k.trim()).filter(k => k);

    const newTarget = {
        name,
        url,
        keywords,
        enabled
    };

    if (editingIndex >= 0) {
        // Keep ID if exists
        if (currentConfig.targets[editingIndex].id) {
            newTarget.id = currentConfig.targets[editingIndex].id;
        }
        currentConfig.targets[editingIndex] = newTarget;
    } else {
        currentConfig.targets.push(newTarget);
    }

    await saveConfig();
    closeModal();
}

// View Switching Logic
window.switchView = function (viewName) {
    // Update Nav
    document.querySelectorAll('.nav-item').forEach(btn => {
        btn.classList.toggle('active', btn.id === 'nav-' + viewName);
    });

    // Update Views
    document.querySelectorAll('.view-section').forEach(section => {
        section.classList.add('hidden');
    });
    document.getElementById('view-' + viewName).classList.remove('hidden');
}
