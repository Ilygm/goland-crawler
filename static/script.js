document.addEventListener('DOMContentLoaded', () => {
    const searchInput = document.getElementById('search-input');
    const searchForm = document.getElementById('search-form');
    const statusArea = document.getElementById('status-area');
    const resultsArea = document.getElementById('results-area');
    const suggestionsContainer = document.getElementById('suggestions-container');

    const API_BASE_URL = window.location.origin;
    let selectedSuggestionIndex = -1;

    // --- Utility: Debounce ---
    function debounce(func, delay) {
        let timeout;
        return function(...args) {
            clearTimeout(timeout);
            timeout = setTimeout(() => func.apply(this, args), delay);
        };
    }

    // --- Autocompletion Logic ---
    async function handleAutocomplete(query) {
        if (query.trim().length < 2) {
            closeSuggestions();
            return;
        }

        try {
            const url = `${API_BASE_URL}/autocomplete?q=${encodeURIComponent(query)}&page=1&size=5`;
            const response = await fetch(url);
            const data = await response.json();

            // Backend returns: { time_taken, results: [{ title, url, suffix }] }
            const results = Array.isArray(data.results) ? data.results : [];
            const suggestions = results.map(r => {
                const suffix = typeof r.suffix === 'string' ? r.suffix : '';
                return query + suffix;
            });

            renderSuggestions(suggestions);
        } catch (error) {
            console.error("Autocomplete failed:", error);
        }
    }

    function renderSuggestions(suggestions) {
        suggestionsContainer.innerHTML = '';
        selectedSuggestionIndex = -1;

        if (suggestions.length === 0) {
            closeSuggestions();
            return;
        }

        suggestions.forEach((suggestion, index) => {
            const li = document.createElement('li');
            li.textContent = suggestion;
            li.addEventListener('click', () => {
                searchInput.value = suggestion;
                closeSuggestions();
                performSearch(suggestion);
            });
            suggestionsContainer.appendChild(li);
        });

        suggestionsContainer.classList.add('active');
    }

    function closeSuggestions() {
        suggestionsContainer.classList.remove('active');
        suggestionsContainer.innerHTML = '';
    }

    // --- Keyboard Navigation for Suggestions ---
    searchInput.addEventListener('keydown', (e) => {
        const items = suggestionsContainer.querySelectorAll('li');
        if (!items.length) return;

        if (e.key === 'ArrowDown') {
            e.preventDefault();
            selectedSuggestionIndex = (selectedSuggestionIndex + 1) % items.length;
            updateSelection(items);
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            selectedSuggestionIndex = (selectedSuggestionIndex - 1 + items.length) % items.length;
            updateSelection(items);
        } else if (e.key === 'Enter' && selectedSuggestionIndex > -1) {
            e.preventDefault();
            searchInput.value = items[selectedSuggestionIndex].textContent;
            closeSuggestions();
            performSearch(searchInput.value);
        }
    });

    function updateSelection(items) {
        items.forEach((item, index) => {
            item.classList.toggle('selected', index === selectedSuggestionIndex);
        });
    }

    // --- Search Logic ---
    async function performSearch(query) {
        if (!query.trim()) return;

        closeSuggestions();
        resultsArea.innerHTML = '';
        statusArea.innerHTML = '<span style="color: var(--primary-color);">در حال جستجو...</span>';

        try {
            const url = `${API_BASE_URL}/search?q=${encodeURIComponent(query)}&page=1&size=10`;
            const response = await fetch(url);
            const data = await response.json();
            renderResults(data);
        } catch (error) {
            console.error("Search failed:", error);
            statusArea.innerHTML = '<span style="color: #dc3545;">خطا در ارتباط با سرور جستجو.</span>';
        }
    }

    searchForm.addEventListener('submit', (e) => {
        e.preventDefault();
        performSearch(searchInput.value);
    });

    searchInput.addEventListener('input', debounce((e) => {
        handleAutocomplete(e.target.value);
    }, 300));

    // Close on outside click
    document.addEventListener('click', (e) => {
        if (!searchForm.contains(e.target) && !suggestionsContainer.contains(e.target)) {
            closeSuggestions();
        }
    });

    // --- Result Rendering ---
    function renderResults(data) {
        resultsArea.innerHTML = '';

        // Backend: { time_taken, total_hits, results: [{ title, url, score }], suggestions?: [] }
        const totalHits = typeof data.total_hits === 'number' ? data.total_hits : 0;
        const time = data.time_taken || '0s';

        let correctionHtml = '';
        const suggestions = Array.isArray(data.suggestions) ? data.suggestions : [];
        const topSuggestion = suggestions.length > 0 ? suggestions[0] : null;
        if (topSuggestion && topSuggestion.trim() !== '' &&
            topSuggestion.trim().toLowerCase() !== searchInput.value.trim().toLowerCase()) {
            correctionHtml = `<div class="correction">آیا منظور شما این بود: <button class="correction-link">${topSuggestion}</button></div>`;
        }

        statusArea.innerHTML = `
            <div>${totalHits.toLocaleString('fa-IR')} نتیجه یافت شد (${time})</div>
            ${correctionHtml}
        `;

        // Handle Correction Click
        const corrBtn = statusArea.querySelector('.correction-link');
        if (corrBtn) {
            corrBtn.addEventListener('click', () => {
                const corrected = corrBtn.textContent || '';
                searchInput.value = corrected;
                performSearch(corrected);
            });
        }

        if (Array.isArray(data.results) && data.results.length > 0) {
            data.results.forEach(hit => {
                const title = hit.title || 'بدون عنوان';
                const url = hit.url || '#';
                const score = typeof hit.score === 'number' ? hit.score.toFixed(2) : null;

                const card = document.createElement('div');
                card.classList.add('result-card');
                card.innerHTML = `
                    <h2><a href="${url}" target="_blank">${title}</a></h2>
                    <span class="result-url">${url !== '#' ? url : ''}</span>
                    ${score !== null ? `<div class="result-meta">امتیاز: ${score}</div>` : ''}
                `;
                resultsArea.appendChild(card);
            });
        } else {
            resultsArea.innerHTML = '<p class="no-results">نتیجه‌ای یافت نشد.</p>';
        }
    }
});