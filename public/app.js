document.addEventListener('DOMContentLoaded', () => {
    // Elements
    const refreshBtn = document.getElementById('refreshBtn');
    const settingsBtn = document.getElementById('settingsBtn');
    const settingsModal = document.getElementById('settingsModal');
    const closeModal = document.querySelector('.close-modal');
    const settingsForm = document.getElementById('settingsForm');
    const loadingOverlay = document.getElementById('loadingOverlay');
    
    const inboxList = document.getElementById('inboxList');
    const historyList = document.getElementById('historyList');
    const inboxCount = document.getElementById('inboxCount');
    const totalAListEl = document.getElementById('totalAList');
    const avgTicketPriceEl = document.getElementById('avgTicketPrice');

    // State
    let currentMovies = [];
    let settings = { username: '', monthlyCost: 30.00 };

    // Initialization
    async function init() {
        await loadSettings();
        if (settings.username) {
            await fetchMovies();
        } else {
            settingsModal.classList.add('active');
        }
    }

    // API Calls
    async function loadSettings() {
        try {
            const res = await fetch('/api/settings');
            if (res.ok) {
                const data = await res.json();
                settings = data;
                document.getElementById('username').value = data.username || '';
                document.getElementById('monthlyCost').value = data.monthlyCost || 30.00;
            }
        } catch (e) {
            console.error('Failed to load settings', e);
        }
    }

    async function saveSettings(e) {
        e.preventDefault();
        const username = document.getElementById('username').value;
        const monthlyCost = parseFloat(document.getElementById('monthlyCost').value);

        showLoading();
        try {
            await fetch('/api/settings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, monthlyCost })
            });
            settings = { username, monthlyCost };
            settingsModal.classList.remove('active');
            await fetchMovies();
        } catch (error) {
            alert('Failed to save settings');
        } finally {
            hideLoading();
        }
    }

    async function fetchMovies() {
        showLoading();
        try {
            const res = await fetch('/api/movies');
            if (res.ok) {
                currentMovies = await res.json();
                renderUI();
            }
        } catch (error) {
            console.error('Failed to fetch movies', error);
        } finally {
            hideLoading();
        }
    }

    async function markMovie(movie, isAList) {
        showLoading();
        try {
            await fetch('/api/mark', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    id: movie.id,
                    title: movie.title,
                    watchedDate: movie.watchedDate,
                    isAList: isAList
                })
            });
            // Update local state
            const m = currentMovies.find(x => x.id === movie.id);
            if (m) {
                m.status = isAList ? 'A-List' : 'Not A-List';
            }
            renderUI();
        } catch (error) {
            console.error('Failed to mark movie', error);
            alert('Failed to save mark');
        } finally {
            hideLoading();
        }
    }

    // Rendering
    function renderUI() {
        const inbox = currentMovies.filter(m => m.status === 'Unmarked');
        const history = currentMovies.filter(m => m.status !== 'Unmarked');

        // Render Inbox
        inboxCount.textContent = inbox.length;
        if (inbox.length === 0) {
            inboxList.innerHTML = '<div class="empty-state">No new movies to review.</div>';
        } else {
            inboxList.innerHTML = '';
            inbox.forEach(movie => {
                const el = createMovieElement(movie, true);
                inboxList.appendChild(el);
            });
        }

        // Render History
        if (history.length === 0) {
            historyList.innerHTML = '<div class="empty-state">No history yet.</div>';
        } else {
            historyList.innerHTML = '';
            // Sort history by date descending
            history.sort((a, b) => new Date(b.watchedDate) - new Date(a.watchedDate)).forEach(movie => {
                const el = createMovieElement(movie, false);
                historyList.appendChild(el);
            });
        }

        updateStats(history);
    }

    function createMovieElement(movie, isInbox) {
        const div = document.createElement('div');
        div.className = 'movie-item';
        
        const dateStr = new Date(movie.watchedDate).toLocaleDateString(undefined, {
            year: 'numeric', month: 'short', day: 'numeric'
        });

        let actionsHtml = '';
        if (isInbox) {
            actionsHtml = `
                <div class="movie-actions">
                    <button class="action-btn btn-alist" title="Mark as A-List">✓</button>
                    <button class="action-btn btn-not" title="Not A-List">✕</button>
                </div>
            `;
        } else {
            const isAList = movie.status === 'A-List';
            actionsHtml = `
                <span class="status-badge ${isAList ? 'status-alist' : 'status-not'}">
                    ${isAList ? 'A-List' : 'Not A-List'}
                </span>
            `;
        }

        div.innerHTML = `
            <div class="movie-info">
                <div class="movie-title"><a href="${movie.link}" target="_blank">${movie.title}</a></div>
                <div class="movie-date">${dateStr}</div>
            </div>
            ${actionsHtml}
        `;

        if (isInbox) {
            div.querySelector('.btn-alist').addEventListener('click', () => markMovie(movie, true));
            div.querySelector('.btn-not').addEventListener('click', () => markMovie(movie, false));
        }

        return div;
    }

    function updateStats(history) {
        const aListMovies = history.filter(m => m.status === 'A-List');
        const count = aListMovies.length;
        totalAListEl.textContent = count;

        if (count === 0 || settings.monthlyCost <= 0) {
            avgTicketPriceEl.textContent = '$0.00';
            return;
        }

        // Calculate months active
        const dates = aListMovies.map(m => new Date(m.watchedDate));
        const minDate = new Date(Math.min(...dates));
        const maxDate = new Date(Math.max(...dates));
        
        let monthsActive = (maxDate.getFullYear() - minDate.getFullYear()) * 12;
        monthsActive -= minDate.getMonth();
        monthsActive += maxDate.getMonth();
        monthsActive += 1; // Inclusive

        if (monthsActive < 1) monthsActive = 1;

        const totalCost = monthsActive * settings.monthlyCost;
        const avgPrice = totalCost / count;

        avgTicketPriceEl.textContent = '$' + avgPrice.toFixed(2);
    }

    // Utilities
    function showLoading() { loadingOverlay.classList.remove('hidden'); }
    function hideLoading() { loadingOverlay.classList.add('hidden'); }

    // Event Listeners
    refreshBtn.addEventListener('click', fetchMovies);
    settingsBtn.addEventListener('click', () => settingsModal.classList.add('active'));
    closeModal.addEventListener('click', () => settingsModal.classList.remove('active'));
    settingsForm.addEventListener('submit', saveSettings);
    
    // Close modal on outside click
    window.addEventListener('click', (e) => {
        if (e.target === settingsModal) {
            settingsModal.classList.remove('active');
        }
    });

    // Start
    init();
});
