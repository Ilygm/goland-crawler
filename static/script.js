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

            renderSuggestions(data.suggestions || []);
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
            const url = `${API_BASE_URL}/correction?q=${encodeURIComponent(query)}&page=1&size=10`;
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

        const totalHits = data.results?.total || 0;
        const time = data.time_taken || '0s';

        let correctionHtml = '';
        if (data.correction && data.correction.trim().toLowerCase() !== searchInput.value.trim().toLowerCase()) {
            correctionHtml = `<div class="correction">آیا منظور شما این بود: <button class="correction-link">${data.correction}</button></div>`;
        }

        statusArea.innerHTML = `
            <div>${totalHits.toLocaleString('fa-IR')} نتیجه یافت شد (${time})</div>
            ${correctionHtml}
        `;

        // Handle Correction Click
        const corrBtn = statusArea.querySelector('.correction-link');
        if (corrBtn) {
            corrBtn.addEventListener('click', () => {
                searchInput.value = data.correction;
                performSearch(data.correction);
            });
        }

        if (data.results?.hits?.length > 0) {
            data.results.hits.forEach(hit => {
                const doc = hit._source || {};
                let snippet = (hit.highlight?.body ? hit.highlight.body[0] : doc.Body) || '';

                const card = document.createElement('div');
                card.classList.add('result-card');
                card.innerHTML = `
                    <h2><a href="${doc.URL || '#'}" target="_blank">${doc.Title || 'بدون عنوان'}</a></h2>
                    <span class="result-url">${doc.URL || ''}</span>
                    <p>${snippet}</p>
                `;
                resultsArea.appendChild(card);
            });
        } else {
            resultsArea.innerHTML = '<p class="no-results">نتیجه‌ای یافت نشد.</p>';
        }
    }
});