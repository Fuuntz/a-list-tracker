document.addEventListener('DOMContentLoaded', () => {
    const elements = {
        authView: document.getElementById('authView'),
        setupView: document.getElementById('setupView'),
        appShell: document.getElementById('appShell'),
        loginForm: document.getElementById('loginForm'),
        loginError: document.getElementById('loginError'),
        setupForm: document.getElementById('setupForm'),
        setupError: document.getElementById('setupError'),
        accountEmail: document.getElementById('accountEmail'),
        logoutBtn: document.getElementById('logoutBtn'),
        settingsBtn: document.getElementById('settingsBtn'),
        settingsModal: document.getElementById('settingsModal'),
        settingsForm: document.getElementById('settingsForm'),
        settingsError: document.getElementById('settingsError'),
        closeModal: document.querySelector('.close-modal'),
        loadingOverlay: document.getElementById('loadingOverlay'),
        refreshBtn: document.getElementById('refreshBtn'),
        syncStatus: document.getElementById('syncStatus'),
        inboxList: document.getElementById('inboxList'),
        historyList: document.getElementById('historyList'),
        inboxCount: document.getElementById('inboxCount'),
        totalAList: document.getElementById('totalAList'),
        avgTicketPrice: document.getElementById('avgTicketPrice'),
        historySearch: document.getElementById('historySearch'),
        historyFilter: document.getElementById('historyFilter'),
        dashboardView: document.getElementById('dashboardView'),
        historyView: document.getElementById('historyView'),
        dataView: document.getElementById('dataView'),
        membershipPeriods: document.getElementById('membershipPeriods'),
        addPeriodBtn: document.getElementById('addPeriodBtn'),
        importForm: document.getElementById('importForm'),
        importPreview: document.getElementById('importPreview'),
        importCounts: document.getElementById('importCounts'),
        importSamples: document.getElementById('importSamples'),
        confirmImportBtn: document.getElementById('confirmImportBtn'),
        showResetBtn: document.getElementById('showResetBtn'),
        resetForm: document.getElementById('resetForm'),
        appMessage: document.getElementById('appMessage')
    };

    let currentMovies = [];
    let settings = { username: '', membershipPeriods: [] };
    let pendingImportJob = null;

    function showOnly(target) {
        [elements.authView, elements.setupView, elements.appShell].forEach(el => el.classList.add('hidden'));
        target.classList.remove('hidden');
    }

    function showError(element, message) {
        element.textContent = message;
        element.classList.toggle('hidden', !message);
    }

    async function api(path, options = {}) {
        const response = await fetch(path, options);
        let body = null;
        try {
            body = await response.json();
        } catch (_) {
            body = null;
        }
        if (!response.ok) {
            const error = new Error(body?.error || `Request failed with status ${response.status}`);
            error.status = response.status;
            if (response.status === 401 && !path.startsWith('/api/auth/')) {
                closeSettings();
                showOnly(elements.authView);
                showError(elements.loginError, 'Your session expired. Sign in again.');
            }
            throw error;
        }
        return body;
    }

    async function initialize() {
        const setupToken = setupTokenFromLocation();
        if (window.location.pathname === '/setup' || setupToken) {
            showOnly(elements.setupView);
            return;
        }

        try {
            const session = await api('/api/auth/session');
            await startApp(session.user);
        } catch (error) {
            showOnly(elements.authView);
            if (error.status !== 401) showError(elements.loginError, error.message);
        }
    }

    async function startApp(user) {
        showOnly(elements.appShell);
        elements.accountEmail.textContent = user.email;
        showLoading();
        try {
            await Promise.all([loadSettings(), loadMovies()]);
            if (!settings.username) openSettings();
        } catch (error) {
            showAppMessage(error.message, true);
        } finally {
            hideLoading();
        }
    }

    async function login(event) {
        event.preventDefault();
        showError(elements.loginError, '');
        const email = document.getElementById('loginEmail').value;
        const password = document.getElementById('loginPassword').value;
        try {
            const result = await api('/api/auth/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ email, password })
            });
            elements.loginForm.reset();
            await startApp(result.user);
        } catch (error) {
            showError(elements.loginError, error.message);
        }
    }

    async function finishSetup(event) {
        event.preventDefault();
        showError(elements.setupError, '');
        const password = document.getElementById('setupPassword').value;
        const confirmation = document.getElementById('setupPasswordConfirm').value;
        if (password !== confirmation) {
            showError(elements.setupError, 'Passwords do not match');
            return;
        }
        const token = setupTokenFromLocation();
        try {
            const result = await api('/api/auth/setup', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ token, password })
            });
            history.replaceState({}, '', '/');
            elements.setupForm.reset();
            await startApp(result.user);
        } catch (error) {
            showError(elements.setupError, error.message);
        }
    }

    function setupTokenFromLocation() {
        const fragmentToken = new URLSearchParams(window.location.hash.slice(1)).get('setup');
        // Query tokens remain accepted for links generated by earlier builds.
        return fragmentToken || new URLSearchParams(window.location.search).get('token') || '';
    }

    async function logout() {
        try {
            await api('/api/auth/logout', { method: 'POST' });
        } finally {
            currentMovies = [];
            settings = { username: '', membershipPeriods: [] };
            showOnly(elements.authView);
        }
    }

    async function loadSettings() {
        settings = await api('/api/settings');
        document.getElementById('username').value = settings.username || '';
        renderMembershipPeriods(settings.membershipPeriods?.length
            ? settings.membershipPeriods
            : [{ startsOn: '', endsOn: null, monthlyCost: 30 }]);
    }

    async function saveSettings(event) {
        event.preventDefault();
        showError(elements.settingsError, '');
        const username = document.getElementById('username').value.trim();
        const payload = {
            username,
            membershipPeriods: collectMembershipPeriods()
        };

        showLoading();
        try {
            await api('/api/settings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });
            settings = payload;
            closeSettings();
            updateStats();
            showAppMessage('Settings saved');
        } catch (error) {
            showError(elements.settingsError, error.message);
        } finally {
            hideLoading();
        }
    }

    function renderMembershipPeriods(periods) {
        elements.membershipPeriods.replaceChildren();
        periods.forEach(period => addMembershipPeriod(period));
    }

    function addMembershipPeriod(period = { startsOn: '', endsOn: null, monthlyCost: 30 }) {
        const row = document.createElement('div');
        row.className = 'period-row';
        row.innerHTML = `
            <label>Start date<input class="period-start" type="date" required></label>
            <label>End date <span>(optional)</span><input class="period-end" type="date"></label>
            <label>Monthly cost<input class="period-cost" type="number" step="0.01" min="0" max="10000" required></label>
            <button type="button" class="remove-period" aria-label="Remove membership period">&times;</button>
        `;
        row.querySelector('.period-start').value = period.startsOn || '';
        row.querySelector('.period-end').value = period.endsOn || '';
        row.querySelector('.period-cost').value = Number(period.monthlyCost ?? 30).toFixed(2);
        row.querySelector('.remove-period').addEventListener('click', () => {
            if (elements.membershipPeriods.children.length > 1) row.remove();
        });
        elements.membershipPeriods.appendChild(row);
    }

    function collectMembershipPeriods() {
        return [...elements.membershipPeriods.querySelectorAll('.period-row')].map(row => {
            const endsOn = row.querySelector('.period-end').value;
            return {
                startsOn: row.querySelector('.period-start').value,
                endsOn: endsOn || null,
                monthlyCost: Number.parseFloat(row.querySelector('.period-cost').value)
            };
        });
    }

    async function loadMovies() {
        currentMovies = await api('/api/movies');
        renderUI();
    }

    async function syncMovies() {
        showLoading();
        elements.syncStatus.textContent = '';
        try {
            const result = await api('/api/sync', { method: 'POST' });
            await loadMovies();
            elements.syncStatus.textContent = `${result.processed} feed entries checked`;
        } catch (error) {
            showAppMessage(error.message, true);
        } finally {
            hideLoading();
        }
    }

    async function updateMovie(movie, status) {
        showLoading();
        try {
            await api('/api/mark', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ id: movie.id, status })
            });
            movie.status = status;
            renderUI();
        } catch (error) {
            showAppMessage(error.message, true);
        } finally {
            hideLoading();
        }
    }

    async function previewImport(event) {
        event.preventDefault();
        const file = document.getElementById('importFile').files[0];
        if (!file) return;
        const form = new FormData();
        form.append('file', file);
        showLoading();
        try {
            const preview = await api('/api/import/preview', { method: 'POST', body: form });
            pendingImportJob = preview.jobId;
            renderImportPreview(preview);
        } catch (error) {
            showAppMessage(error.message, true);
        } finally {
            hideLoading();
        }
    }

    function renderImportPreview(preview) {
        elements.importCounts.replaceChildren();
        [
            ['Eligible', preview.eligible],
            ['Already saved', preview.duplicates],
            ['Before membership', preview.tooOld],
            ['Invalid', preview.invalid]
        ].forEach(([label, count]) => {
            const item = document.createElement('div');
            const value = document.createElement('strong');
            value.textContent = count;
            const text = document.createElement('span');
            text.textContent = label;
            item.append(value, text);
            elements.importCounts.appendChild(item);
        });

        elements.importSamples.replaceChildren();
        preview.samples.forEach(sample => {
            const row = document.createElement('div');
            const title = document.createElement('span');
            title.textContent = sample.title;
            const result = document.createElement('span');
            result.textContent = `${sample.watchedDate || 'No date'} · ${sample.disposition.replace('_', ' ')}`;
            row.append(title, result);
            elements.importSamples.appendChild(row);
        });
        elements.confirmImportBtn.disabled = preview.eligible === 0;
        elements.confirmImportBtn.textContent = preview.eligible === 0
            ? 'Nothing to import'
            : `Import ${preview.eligible} viewing${preview.eligible === 1 ? '' : 's'}`;
        elements.importPreview.classList.remove('hidden');
    }

    async function confirmImport() {
        if (!pendingImportJob) return;
        showLoading();
        try {
            const result = await api('/api/import/confirm', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ jobId: pendingImportJob })
            });
            pendingImportJob = null;
            elements.importForm.reset();
            elements.importPreview.classList.add('hidden');
            await loadMovies();
            showAppMessage(`Imported ${result.imported} viewing${result.imported === 1 ? '' : 's'}`);
            switchView('history');
        } catch (error) {
            showAppMessage(error.message, true);
        } finally {
            hideLoading();
        }
    }

    async function resetTrackerData(event) {
        event.preventDefault();
        const confirmation = document.getElementById('resetConfirmation').value;
        showLoading();
        try {
            await api('/api/data/reset', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ confirmation })
            });
            currentMovies = [];
            settings = { username: '', membershipPeriods: [] };
            pendingImportJob = null;
            elements.resetForm.reset();
            elements.resetForm.classList.add('hidden');
            elements.importPreview.classList.add('hidden');
            renderUI();
            renderMembershipPeriods([{ startsOn: '', endsOn: null, monthlyCost: 30 }]);
            document.getElementById('username').value = '';
            switchView('dashboard');
            openSettings();
            showAppMessage('Tracker data reset. Your login account was kept.');
        } catch (error) {
            showAppMessage(error.message, true);
        } finally {
            hideLoading();
        }
    }

    function renderUI() {
        const inbox = currentMovies.filter(movie => movie.status === 'unreviewed');
        elements.inboxCount.textContent = inbox.length;
        renderMovieList(elements.inboxList, inbox, true);
        renderHistory();
        updateStats();
    }

    function renderHistory() {
        const query = elements.historySearch.value.trim().toLocaleLowerCase();
        const filter = elements.historyFilter.value;
        const movies = currentMovies.filter(movie => {
            const matchesQuery = !query || movie.title.toLocaleLowerCase().includes(query);
            const matchesFilter = filter === 'all' || movie.status === filter;
            return matchesQuery && matchesFilter;
        });
        renderMovieList(elements.historyList, movies, false);
    }

    function renderMovieList(container, movies, inbox) {
        container.replaceChildren();
        if (movies.length === 0) {
            const empty = document.createElement('div');
            empty.className = 'empty-state panel';
            empty.textContent = inbox ? 'No new movies to review.' : 'No matching history.';
            container.appendChild(empty);
            return;
        }
        movies.forEach(movie => container.appendChild(createMovieElement(movie, inbox)));
    }

    function createMovieElement(movie, inbox) {
        const row = document.createElement('article');
        row.className = 'movie-item panel';

        const info = document.createElement('div');
        info.className = 'movie-info';
        const title = document.createElement('div');
        title.className = 'movie-title';
        const safeLink = validatedLetterboxdLink(movie.link);
        if (safeLink) {
            const anchor = document.createElement('a');
            anchor.href = safeLink;
            anchor.target = '_blank';
            anchor.rel = 'noopener noreferrer';
            anchor.textContent = movie.title;
            title.appendChild(anchor);
        } else {
            title.textContent = movie.title;
        }
        const date = document.createElement('div');
        date.className = 'movie-date';
        const parsedDate = new Date(`${movie.watchedDate}T00:00:00`);
        date.textContent = parsedDate.toLocaleDateString(undefined, {
            year: 'numeric', month: 'short', day: 'numeric'
        });
        info.append(title, date);
        row.appendChild(info);

        if (inbox) {
            const actions = document.createElement('div');
            actions.className = 'movie-actions';
            actions.append(
                actionButton('A-List', 'btn-alist', () => updateMovie(movie, 'a_list')),
                actionButton('Not A-List', 'btn-not', () => updateMovie(movie, 'not_a_list'))
            );
            row.appendChild(actions);
        } else {
            const label = document.createElement('label');
            label.className = 'visually-hidden';
            label.htmlFor = `status-${movie.id}`;
            label.textContent = `Classification for ${movie.title}`;
            const select = document.createElement('select');
            select.id = `status-${movie.id}`;
            select.className = `status-select status-${movie.status}`;
            [
                ['unreviewed', 'Needs review'],
                ['a_list', 'A-List'],
                ['not_a_list', 'Not A-List'],
                ['excluded', 'Excluded']
            ].forEach(([value, text]) => {
                const option = document.createElement('option');
                option.value = value;
                option.textContent = text;
                select.appendChild(option);
            });
            select.value = movie.status;
            select.addEventListener('change', () => updateMovie(movie, select.value));
            row.append(label, select);
        }
        return row;
    }

    function actionButton(label, className, handler) {
        const button = document.createElement('button');
        button.type = 'button';
        button.className = `action-btn ${className}`;
        button.textContent = label;
        button.addEventListener('click', handler);
        return button;
    }

    function validatedLetterboxdLink(value) {
        if (!value) return null;
        try {
            const parsed = new URL(value);
            if (parsed.protocol !== 'https:') return null;
            if (parsed.hostname === 'letterboxd.com' || parsed.hostname.endsWith('.letterboxd.com') || parsed.hostname === 'boxd.it') {
                return parsed.href;
            }
        } catch (_) {
            return null;
        }
        return null;
    }

    function updateStats() {
        const aListCount = currentMovies.filter(movie => movie.status === 'a_list').length;
        elements.totalAList.textContent = aListCount;
        const cost = membershipCostThroughToday(settings.membershipPeriods || []);
        elements.avgTicketPrice.textContent = aListCount > 0 && cost > 0
            ? `$${(cost / aListCount).toFixed(2)}`
            : '—';
    }

    function membershipCostThroughToday(periods) {
        const today = new Date();
        return periods.reduce((sum, period) => {
            const start = new Date(`${period.startsOn}T00:00:00`);
            if (Number.isNaN(start.getTime()) || start > today) return sum;
            const requestedEnd = period.endsOn ? new Date(`${period.endsOn}T00:00:00`) : today;
            const end = requestedEnd > today ? today : requestedEnd;
            if (end < start) return sum;
            return sum + countMonthlyRenewals(start, end) * Number(period.monthlyCost || 0);
        }, 0);
    }

    function countMonthlyRenewals(start, end) {
        let charges = 0;
        while (charges < 1200) {
            const targetMonth = start.getMonth() + charges;
            const firstOfMonth = new Date(start.getFullYear(), targetMonth, 1);
            const lastDay = new Date(firstOfMonth.getFullYear(), firstOfMonth.getMonth() + 1, 0).getDate();
            const renewal = new Date(firstOfMonth.getFullYear(), firstOfMonth.getMonth(), Math.min(start.getDate(), lastDay));
            if (renewal > end) break;
            charges++;
        }
        return charges;
    }

    function switchView(view) {
        const history = view === 'history';
        const data = view === 'data';
        elements.dashboardView.classList.toggle('hidden', history || data);
        elements.historyView.classList.toggle('hidden', !history);
        elements.dataView.classList.toggle('hidden', !data);
        document.querySelectorAll('.nav-button').forEach(button => {
            button.classList.toggle('active', button.dataset.view === view);
        });
    }

    function openSettings() {
        elements.settingsModal.classList.add('active');
        document.getElementById('username').focus();
    }

    function closeSettings() {
        elements.settingsModal.classList.remove('active');
    }

    function showAppMessage(message, error = false) {
        elements.appMessage.textContent = message;
        elements.appMessage.classList.toggle('error', error);
        elements.appMessage.classList.remove('hidden');
        window.setTimeout(() => elements.appMessage.classList.add('hidden'), 5000);
    }

    function showLoading() { elements.loadingOverlay.classList.remove('hidden'); }
    function hideLoading() { elements.loadingOverlay.classList.add('hidden'); }

    elements.loginForm.addEventListener('submit', login);
    elements.setupForm.addEventListener('submit', finishSetup);
    elements.logoutBtn.addEventListener('click', logout);
    elements.settingsBtn.addEventListener('click', openSettings);
    elements.closeModal.addEventListener('click', closeSettings);
    elements.settingsForm.addEventListener('submit', saveSettings);
    elements.addPeriodBtn.addEventListener('click', () => addMembershipPeriod());
    elements.refreshBtn.addEventListener('click', syncMovies);
    elements.importForm.addEventListener('submit', previewImport);
    elements.confirmImportBtn.addEventListener('click', confirmImport);
    elements.showResetBtn.addEventListener('click', () => {
        elements.resetForm.classList.toggle('hidden');
        if (!elements.resetForm.classList.contains('hidden')) {
            document.getElementById('resetConfirmation').focus();
        }
    });
    elements.resetForm.addEventListener('submit', resetTrackerData);
    elements.historySearch.addEventListener('input', renderHistory);
    elements.historyFilter.addEventListener('change', renderHistory);
    document.querySelectorAll('.nav-button').forEach(button => {
        button.addEventListener('click', () => switchView(button.dataset.view));
    });
    elements.settingsModal.addEventListener('click', event => {
        if (event.target === elements.settingsModal) closeSettings();
    });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape') closeSettings();
    });

    initialize();
});
