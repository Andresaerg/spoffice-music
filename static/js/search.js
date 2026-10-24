// Search form autocomplete, suggestions, and keyboard navigation
window.MusicApp = window.MusicApp || {};

(function (app) {
    let suggestions = [];
    let activeSuggestionIndex = -1;
    let suggestionDebounceTimer;

    const getSearchForm = () => document.querySelector('#search-form');
    const getSearchInput = () => document.querySelector('#search-input');
    const getSuggestionsDropdown = () => document.querySelector('#suggestions-dropdown');

    app.hideSuggestions = function () {
        const dropdown = getSuggestionsDropdown();
        if (!dropdown) return;
        dropdown.classList.add('hidden');
        dropdown.replaceChildren();
        suggestions = [];
        activeSuggestionIndex = -1;
    };

    app.showSuggestions = function (list) {
        const dropdown = getSuggestionsDropdown();
        if (!dropdown) return;

        suggestions = list;
        activeSuggestionIndex = -1;
        dropdown.replaceChildren();

        if (!list || list.length === 0) {
            app.hideSuggestions();
            return;
        }

        for (let i = 0; i < list.length; i++) {
            const text = list[i];
            const li = document.createElement('li');
            li.className = 'flex items-center gap-3 px-4 py-2.5 text-sm text-slate-200 hover:bg-white/10 hover:text-white cursor-pointer transition-colors';
            li.dataset.index = i;
            li.innerHTML = `
                <svg class="size-4 shrink-0 text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
                </svg>
                <span class="truncate">${text}</span>
            `;

            li.addEventListener('mousedown', (e) => {
                e.preventDefault();
                app.selectSuggestion(text);
            });

            dropdown.appendChild(li);
        }

        dropdown.classList.remove('hidden');
    };

    app.selectSuggestion = function (text) {
        const input = getSearchInput();
        const form = getSearchForm();
        if (!input) return;
        input.value = text;
        app.hideSuggestions();
        if (window.htmx && form) {
            htmx.trigger(form, 'submit');
        }
    };

    function updateActiveSuggestion() {
        const dropdown = getSuggestionsDropdown();
        if (!dropdown) return;
        const items = dropdown.querySelectorAll('li');
        items.forEach((item, idx) => {
            if (idx === activeSuggestionIndex) {
                item.classList.add('bg-white/10', 'text-white');
                item.scrollIntoView({ block: 'nearest' });
            } else {
                item.classList.remove('bg-white/10', 'text-white');
            }
        });
    }

    async function fetchSuggestions(query) {
        try {
            const response = await fetch(`/search/suggestions?query=${encodeURIComponent(query)}`);
            if (!response.ok) return;
            const data = await response.json();
            const input = getSearchInput();
            if (input && input.value.trim() === query) {
                app.showSuggestions(data);
            }
        } catch (err) {
            console.error(err);
        }
    }

    app.initSearch = function () {
        const input = getSearchInput();
        const form = getSearchForm();

        if (input) {
            input.addEventListener('input', () => {
                clearTimeout(suggestionDebounceTimer);
                const query = input.value.trim();
                if (query.length < 2) {
                    app.hideSuggestions();
                    return;
                }
                suggestionDebounceTimer = setTimeout(() => fetchSuggestions(query), 180);
            });

            input.addEventListener('keydown', (e) => {
                const dropdown = getSuggestionsDropdown();
                if (!dropdown || dropdown.classList.contains('hidden') || suggestions.length === 0) {
                    if (e.key === 'Escape') app.hideSuggestions();
                    return;
                }

                if (e.key === 'ArrowDown') {
                    e.preventDefault();
                    activeSuggestionIndex = (activeSuggestionIndex + 1) % suggestions.length;
                    updateActiveSuggestion();
                } else if (e.key === 'ArrowUp') {
                    e.preventDefault();
                    activeSuggestionIndex = (activeSuggestionIndex - 1 + suggestions.length) % suggestions.length;
                    updateActiveSuggestion();
                } else if (e.key === 'Enter') {
                    if (activeSuggestionIndex >= 0 && activeSuggestionIndex < suggestions.length) {
                        e.preventDefault();
                        app.selectSuggestion(suggestions[activeSuggestionIndex]);
                    } else {
                        app.hideSuggestions();
                    }
                } else if (e.key === 'Escape') {
                    app.hideSuggestions();
                }
            });
        }

        document.addEventListener('click', (e) => {
            const input = getSearchInput();
            const dropdown = getSuggestionsDropdown();
            if (input && dropdown && !input.contains(e.target) && !dropdown.contains(e.target)) {
                app.hideSuggestions();
            }
        });

        if (form) {
            form.addEventListener('submit', () => {
                app.hideSuggestions();
                const input = getSearchInput();
                const query = input ? input.value.trim() : '';
                if (query) {
                    const titleEl = document.querySelector('#results-title');
                    if (titleEl) titleEl.textContent = `Resultados para "${query}"`;
                }
            });
        }
    };

})(window.MusicApp);
