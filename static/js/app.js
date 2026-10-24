// Application orchestration, SPA view transitions, toast notifications, and event binding
window.MusicApp = window.MusicApp || {};

(function (app) {
    const appConfigEl = document.querySelector('#app-config');
    let currentView = appConfigEl?.dataset.currentPage === 'list' ? 'search' : 'player';
    let toastTimeout;

    // Toast notifications
    app.scheduleToastDismiss = function (toastElement) {
        const toast = toastElement || document.querySelector('#toast');
        if (!toast) return;
        clearTimeout(toastTimeout);
        toast.onclick = () => toast.classList.add('hidden');
        toastTimeout = setTimeout(() => {
            toast.classList.add('hidden');
        }, 3500);
    };

    // SPA View Switching & Mini Player Logic
    app.switchView = function (view, pushState = true) {
        currentView = view;

        const searchView = document.querySelector('#search-view');
        const playerGridView = document.querySelector('#player-grid-view');
        const playerSection = document.querySelector('#player-section');
        const queueSection = document.querySelector('#queue-section');
        const miniPlayerHeader = document.querySelector('#mini-player-header');
        const fullPlayerHeader = document.querySelector('#full-player-header');
        const fullPlayerFooter = document.querySelector('#full-player-footer');
        const viewToggleBtn = document.querySelector('#view-toggle-btn');
        const headerSubtitle = document.querySelector('#header-subtitle');

        if (view === 'search') {
            searchView?.classList.remove('hidden');
            if (playerGridView) playerGridView.className = 'contents';
            queueSection?.classList.add('hidden');

            if (playerSection) {
                playerSection.className = 'fixed bottom-5 right-5 z-50 w-80 sm:w-96 rounded-xl border border-white/20 bg-[#181818]/95 shadow-2xl shadow-black/90 backdrop-blur-md overflow-hidden ring-1 ring-black/50 transition-all duration-300';
            }
            miniPlayerHeader?.classList.remove('hidden');
            fullPlayerHeader?.classList.add('hidden');
            fullPlayerFooter?.classList.add('hidden');

            if (viewToggleBtn) {
                viewToggleBtn.textContent = 'Abrir reproductor';
                viewToggleBtn.href = '/';
            }
            if (headerSubtitle) {
                headerSubtitle.textContent = 'Busca canciones y administra la cola de hoy';
            }

            if (pushState) {
                history.pushState({ view: 'search' }, '', '/list');
            }
        } else {
            searchView?.classList.add('hidden');
            if (playerGridView) {
                playerGridView.className = 'grid flex-1 gap-5 lg:h-[calc(100vh-9rem)] lg:min-h-[32rem] lg:grid-cols-[minmax(0,2.2fr)_minmax(20rem,1fr)]';
            }
            queueSection?.classList.remove('hidden');

            if (playerSection) {
                playerSection.className = 'flex min-h-0 flex-col min-w-0 transition-all duration-300';
            }
            miniPlayerHeader?.classList.add('hidden');
            fullPlayerHeader?.classList.remove('hidden');
            fullPlayerFooter?.classList.remove('hidden');

            if (viewToggleBtn) {
                viewToggleBtn.textContent = 'Buscar canciones';
                viewToggleBtn.href = '/list';
            }
            if (headerSubtitle) {
                headerSubtitle.textContent = 'Lista de reproducción · canciones de hoy';
            }

            if (pushState) {
                history.pushState({ view: 'player' }, '', '/');
            }
        }
    };

    // Initialize event listeners when DOM is loaded
    function init() {
        // Search module initialization
        if (app.initSearch) {
            app.initSearch();
        }

        // View toggle buttons
        const viewToggleBtn = document.querySelector('#view-toggle-btn');
        if (viewToggleBtn) {
            viewToggleBtn.addEventListener('click', (e) => {
                e.preventDefault();
                app.switchView(currentView === 'player' ? 'search' : 'player');
            });
        }

        const brandLink = document.querySelector('#brand-link');
        if (brandLink) {
            brandLink.addEventListener('click', (e) => {
                e.preventDefault();
                if (currentView !== 'player') {
                    app.switchView('player');
                }
            });
        }

        const miniExpandBtn = document.querySelector('#mini-expand-btn');
        if (miniExpandBtn) {
            miniExpandBtn.addEventListener('click', () => app.switchView('player'));
        }

        const miniSkipBtn = document.querySelector('#mini-skip-btn');
        if (miniSkipBtn) {
            miniSkipBtn.addEventListener('click', () => app.skipCurrentSong().catch(console.error));
        }

        window.addEventListener('popstate', () => {
            const isList = window.location.pathname.startsWith('/list');
            app.switchView(isList ? 'search' : 'player', false);
        });

        // Queue action buttons
        const shuffleBtn = document.querySelector('#shuffle-queue');
        if (shuffleBtn) {
            shuffleBtn.addEventListener('click', () => app.shuffleQueue().catch(console.error));
        }

        const reorderBtn = document.querySelector('#reorder-queue');
        if (reorderBtn) {
            reorderBtn.addEventListener('click', app.toggleReorderMode);
        }

        const refreshBtn = document.querySelector('#refresh-queue');
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => app.refreshQueue().catch(console.error));
        }

        const randomBtn = document.querySelector('#random-queue');
        if (randomBtn) {
            randomBtn.addEventListener('click', () => app.addRandomSongs().catch(console.error));
        }

        // Player engine buttons
        const engineYtBtn = app.getEngineYtBtn();
        if (engineYtBtn) {
            engineYtBtn.addEventListener('click', () => app.switchPlayerEngine('youtube'));
        }

        const engineInvidiousBtn = app.getEngineInvidiousBtn();
        if (engineInvidiousBtn) {
            engineInvidiousBtn.addEventListener('click', () => app.switchPlayerEngine('invidious'));
        }

        const skipSongBtn = app.getSkipSongBtn();
        if (skipSongBtn) {
            skipSongBtn.addEventListener('click', () => app.skipCurrentSong().catch(console.error));
        }

        app.updateEngineUI();

        // HTMX & Toast Event Listeners
        document.body.addEventListener('htmx:oobAfterSwap', () => {
            const toast = document.querySelector('#toast');
            if (toast && !toast.classList.contains('hidden')) {
                app.scheduleToastDismiss(toast);
            }
        });

        document.body.addEventListener('htmx:afterSettle', () => {
            const toast = document.querySelector('#toast');
            if (toast && !toast.classList.contains('hidden') && toast.textContent.trim()) {
                app.scheduleToastDismiss(toast);
            }
        });

        document.addEventListener('click', (e) => {
            const toast = e.target.closest('#toast');
            if (toast) {
                toast.classList.add('hidden');
            }
        });
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }

})(window.MusicApp);
