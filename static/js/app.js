// Application orchestration, SPA view transitions, toast notifications, and event binding
window.MusicApp = window.MusicApp || {};

(function (app) {
    const appConfigEl = document.querySelector('#app-config');
    let currentView = appConfigEl?.dataset.currentPage === 'list' ? 'search' : 'player';
    let toastTimeout;

    // Toast notifications (auto-dismiss after 3 seconds)
    app.hideToast = function (toastElement) {
        const toast = toastElement || document.querySelector('#toast');
        if (!toast) return;
        clearTimeout(toastTimeout);
        toast.classList.add('hidden');
        toast.classList.remove('flex');
        toast.style.display = 'none';
    };

    app.scheduleToastDismiss = function (toastElement, durationMs = 3000) {
        const toast = toastElement || document.querySelector('#toast');
        if (!toast) return;
        clearTimeout(toastTimeout);
        toast.style.display = '';
        toast.classList.remove('hidden');
        toast.onclick = () => app.hideToast(toast);
        toastTimeout = setTimeout(() => {
            app.hideToast(toast);
        }, durationMs);
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
                playerSection.className = 'fixed inset-x-3 bottom-3 z-50 sm:left-auto sm:right-5 sm:bottom-5 sm:w-96 rounded-xl border border-white/20 bg-[#181818]/95 shadow-2xl shadow-black/90 backdrop-blur-md overflow-hidden ring-1 ring-black/50 transition-all duration-300';
            }
            miniPlayerHeader?.classList.remove('hidden');
            fullPlayerHeader?.classList.add('hidden');
            fullPlayerFooter?.classList.add('hidden');

            if (viewToggleBtn) {
                viewToggleBtn.innerHTML = `
                    <svg class="size-4" fill="currentColor" viewBox="0 0 24 24">
                        <path d="M8 5v14l11-7z"></path>
                    </svg>
                    <span id="view-toggle-label">Abrir reproductor</span>
                `;
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
                playerGridView.className = 'grid flex-1 grid-cols-[minmax(0,1fr)] gap-4 lg:h-[calc(100vh-8.5rem)] lg:min-h-[32rem] lg:grid-cols-[minmax(0,2.1fr)_minmax(22rem,1fr)] lg:gap-6';
            }
            queueSection?.classList.remove('hidden');

            if (playerSection) {
                playerSection.className = 'flex min-h-0 flex-col min-w-0 transition-all duration-300';
            }
            miniPlayerHeader?.classList.add('hidden');
            fullPlayerHeader?.classList.remove('hidden');
            fullPlayerFooter?.classList.remove('hidden');

            if (viewToggleBtn) {
                viewToggleBtn.innerHTML = `
                    <svg class="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"></path>
                    </svg>
                    <span id="view-toggle-label">Buscar canciones</span>
                `;
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

        const clearPlayedBtn = document.querySelector('#clear-played-queue');
        if (clearPlayedBtn) {
            clearPlayedBtn.addEventListener('click', () => app.clearPlayedSongs().catch(console.error));
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

        app.updateEngineUI();

        // HTMX & Toast Event Listeners (only schedule dismiss when #toast itself is swapped, not on 1.5s SSE ticks)
        document.body.addEventListener('htmx:oobAfterSwap', (event) => {
            if (event.detail?.target?.id === 'toast') {
                app.scheduleToastDismiss(event.detail.target);
            }
        });

        document.addEventListener('click', (e) => {
            const toast = e.target.closest('#toast');
            if (toast) {
                app.hideToast(toast);
            }
        });
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }

})(window.MusicApp);
