// Queue management, drag-and-drop reordering, and search card status synchronization
window.MusicApp = window.MusicApp || {};

(function (app) {
    app.isReordering = false;
    app.draggedItem = null;

    const getQueueContainer = () => document.querySelector('#queue-container');

    let actionToastTimer = null;

    app.showActionToast = function (message, isError = false) {
        const toast = document.querySelector('#toast');
        if (!toast) return;
        toast.style.display = '';
        toast.className = isError
            ? 'fixed top-5 right-5 z-[100] cursor-pointer rounded-xl bg-red-600 px-4 py-3 text-sm font-medium text-white shadow-2xl transition-all duration-300 select-none'
            : 'fixed top-5 right-5 z-[100] cursor-pointer rounded-xl bg-emerald-600 px-4 py-3 text-sm font-medium text-white shadow-2xl transition-all duration-300 select-none';
        toast.textContent = message;
        clearTimeout(actionToastTimer);
        if (app.scheduleToastDismiss) {
            app.scheduleToastDismiss(toast, 3000);
        }
        actionToastTimer = setTimeout(() => {
            if (app.hideToast) {
                app.hideToast(toast);
            } else {
                toast.classList.add('hidden');
                toast.style.display = 'none';
            }
        }, 3000);
    };

    app.scrollToActiveTrack = function () {
        if (app.isReordering) return;
        const list = getQueueContainer();
        if (!list) return;
        const activeBtn = list.querySelector('button[aria-current="true"]');
        const activeLi = activeBtn?.closest('li');
        if (activeLi) {
            activeLi.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
    };

    app.refreshQueue = async function () {
        const response = await fetch('/queue/refresh', { method: 'POST' });
        if (!response.ok) throw new Error('No se pudo actualizar la cola');
        setTimeout(() => app.scrollToActiveTrack(), 120);
        app.showActionToast('Cola sincronizada');
    };

    app.addRandomSongs = async function () {
        const button = document.querySelector('#random-queue');
        const status = document.querySelector('#random-queue-status');
        if (!button) return;

        button.disabled = true;
        if (status) status.textContent = '';

        try {
            const response = await fetch('/queue/random', { method: 'POST' });
            if (!response.ok) throw new Error('No se pudieron agregar canciones aleatorias');
            const result = await response.json();

            const msg = result.added > 0
                ? `Se agregaron ${result.added} canciones al mix de hoy`
                : 'Todas las canciones guardadas ya están en la cola de hoy';
            app.showActionToast(msg);
        } catch (error) {
            app.showActionToast('No se pudieron agregar canciones aleatorias', true);
            throw error;
        } finally {
            button.disabled = false;
        }
    };

    app.clearPlayedSongs = async function () {
        const button = document.querySelector('#clear-played-queue');
        if (button) button.disabled = true;

        try {
            const response = await fetch('/room/queue/clear-played', { method: 'POST' });
            if (!response.ok) throw new Error('No se pudieron limpiar las canciones reproducidas');
            const result = await response.json();
            const msg = result.removed > 0
                ? `Se limpiaron ${result.removed} canciones ya reproducidas`
                : 'No hay canciones reproducidas para limpiar';
            app.showActionToast(msg);
        } catch (error) {
            app.showActionToast('Error al limpiar canciones reproducidas', true);
            throw error;
        } finally {
            if (button) button.disabled = false;
        }
    };

    app.syncSearchResultsQueueStatus = function () {
        const listEl = getQueueContainer();
        if (!listEl) return;

        const inQueueIds = new Set(
            Array.from(listEl.querySelectorAll('li[data-video-id]'))
                .filter(li => li.dataset.status !== 'played')
                .map(li => li.dataset.videoId)
                .filter(Boolean)
        );

        const forms = document.querySelectorAll('#search-results form[data-video-id]');
        forms.forEach(form => {
            const videoId = form.dataset.videoId;
            if (!videoId) return;

            const isInQueue = inQueueIds.has(videoId);
            const disabledBtn = form.querySelector('button[disabled]');

            if (isInQueue && !disabledBtn) {
                const vidInput = form.querySelector('input[name="video_id"]')?.value || videoId;
                const titleInput = form.querySelector('input[name="title"]')?.value || '';
                const descInput = form.querySelector('input[name="description"]')?.value || '';
                const thumbInput = form.querySelector('input[name="thumbnail"]')?.value || '';

                form.innerHTML = `
                    <input type="hidden" name="video_id" value="${vidInput}">
                    <input type="hidden" name="title" value="${titleInput.replace(/"/g, '&quot;')}">
                    <input type="hidden" name="description" value="${descInput.replace(/"/g, '&quot;')}">
                    <input type="hidden" name="thumbnail" value="${thumbInput.replace(/"/g, '&quot;')}">
                    <button disabled type="button" class="cursor-default inline-flex items-center gap-1.5 rounded-full border border-white/10 bg-white/5 px-3.5 py-1.5 text-xs font-semibold text-slate-400">
                        <svg class="size-3.5 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg>
                        <span>Ya en la cola</span>
                    </button>
                `;
            } else if (!isInQueue && disabledBtn) {
                const vidInput = form.querySelector('input[name="video_id"]')?.value || videoId;
                const titleInput = form.querySelector('input[name="title"]')?.value || '';
                const descInput = form.querySelector('input[name="description"]')?.value || '';
                const thumbInput = form.querySelector('input[name="thumbnail"]')?.value || '';

                form.innerHTML = `
                    <input type="hidden" name="video_id" value="${vidInput}">
                    <input type="hidden" name="title" value="${titleInput.replace(/"/g, '&quot;')}">
                    <input type="hidden" name="description" value="${descInput.replace(/"/g, '&quot;')}">
                    <input type="hidden" name="thumbnail" value="${thumbInput.replace(/"/g, '&quot;')}">
                    <button
                        type="submit"
                        hx-post="/add-to-queue"
                        hx-include="closest li"
                        hx-target="closest form"
                        hx-swap="outerHTML"
                        class="inline-flex cursor-pointer items-center gap-1.5 rounded-full bg-white px-3.5 py-1.5 text-xs font-semibold text-black shadow transition-all hover:bg-slate-200"
                        title="Agregar al final de la cola"
                    >
                        <svg class="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M12 4v16m8-8H4"/></svg>
                        <span>En cola</span>
                    </button>
                    <button
                        type="submit"
                        hx-post="/add-to-queue?next=true"
                        hx-include="closest li"
                        hx-target="closest form"
                        hx-swap="outerHTML"
                        class="inline-flex cursor-pointer items-center gap-1.5 rounded-full border border-white/15 bg-white/10 px-3.5 py-1.5 text-xs font-semibold text-white transition-all hover:border-white/30 hover:bg-white/20"
                        title="Reproducir inmediatamente después de la canción actual"
                    >
                        <svg class="size-3.5 shrink-0 text-red-400" fill="currentColor" viewBox="0 0 24 24">
                            <path d="M6 18l8.5-6L6 6v12zM16 6v12h2V6h-2z" />
                        </svg>
                        <span>Siguiente</span>
                    </button>
                `;
                if (window.htmx) {
                    htmx.process(form);
                }
            }
        });
    };

    let isApplyingHandles = false;

    app.enableDragHandles = function () {
        if (!app.isReordering || isApplyingHandles) return;
        const list = getQueueContainer();
        if (!list) return;

        isApplyingHandles = true;
        try {
            Array.from(list.querySelectorAll('li[data-id]')).forEach(item => {
                item.draggable = true;
                item.classList.add('cursor-grab', 'active:cursor-grabbing');

                if (!item.querySelector('.drag-handle')) {
                    const handle = document.createElement('span');
                    handle.className = 'drag-handle select-none px-1.5 text-slate-400 hover:text-white shrink-0 font-bold text-base';
                    handle.textContent = '⋮⋮';
                    handle.title = 'Arrastra para mover';
                    item.prepend(handle);
                }

                item.ondragstart = (e) => {
                    app.draggedItem = item;
                    e.dataTransfer.effectAllowed = 'move';
                    e.dataTransfer.setData('text/plain', item.dataset.id);
                    setTimeout(() => item.classList.add('opacity-40', 'bg-white/5'), 0);
                };

                item.ondragend = () => {
                    item.classList.remove('opacity-40', 'bg-white/5');
                    app.draggedItem = null;
                    app.saveNewOrder().catch(console.error);
                };

                item.ondragover = (e) => {
                    e.preventDefault();
                    e.dataTransfer.dropEffect = 'move';
                    if (!app.draggedItem || app.draggedItem === item) return;
                    const rect = item.getBoundingClientRect();
                    const midY = rect.top + rect.height / 2;
                    if (e.clientY < midY) {
                        list.insertBefore(app.draggedItem, item);
                    } else {
                        list.insertBefore(app.draggedItem, item.nextSibling);
                    }
                };
            });
        } finally {
            isApplyingHandles = false;
        }
    };

    app.disableDragHandles = function () {
        const list = getQueueContainer();
        if (!list) return;
        Array.from(list.querySelectorAll('li[data-id]')).forEach(item => {
            item.draggable = false;
            item.classList.remove('cursor-grab', 'active:cursor-grabbing');
            item.ondragstart = null;
            item.ondragend = null;
            item.ondragover = null;
            item.querySelector('.drag-handle')?.remove();
        });
    };

    app.saveNewOrder = async function () {
        const list = getQueueContainer();
        if (!list) return;

        const ids = Array.from(list.querySelectorAll('li[data-id]'))
            .map(el => Number(el.dataset.id))
            .filter(id => !isNaN(id) && id > 0);

        if (ids.length === 0) return;

        const response = await fetch('/queue/reorder', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ids })
        });
        if (!response.ok) throw new Error('No se pudo guardar el nuevo orden');
        if (app.isReordering) {
            app.enableDragHandles();
        }
    };

    app.toggleReorderMode = async function () {
        app.isReordering = !app.isReordering;
        const btn = document.querySelector('#reorder-queue');
        const label = document.querySelector('#reorder-queue-label') || btn;
        if (!btn) return;

        if (app.isReordering) {
            if (label) label.textContent = 'Listo';
            btn.classList.add('bg-white/20', 'text-white', 'border-white/40', 'font-semibold');
            app.enableDragHandles();
        } else {
            if (label) label.textContent = 'Reordenar';
            btn.classList.remove('bg-white/20', 'text-white', 'border-white/40', 'font-semibold');
            app.disableDragHandles();
            await app.refreshQueue();
        }
    };

    app.shuffleQueue = async function () {
        const list = getQueueContainer();
        if (!list) return;

        const items = Array.from(list.querySelectorAll('li[data-id]'));
        if (items.length <= 1) return;

        const button = document.querySelector('#shuffle-queue');
        if (button) button.disabled = true;

        try {
            // Smart Shuffle: keep already-played history and currently playing track in place,
            // and shuffle all upcoming ('pending') tracks! If all tracks are already played, shuffle non-playing tracks.
            const played = items.filter(el => el.dataset.status === 'played');
            const playing = items.filter(el => el.dataset.status === 'playing');
            let upcoming = items.filter(el => el.dataset.status !== 'played' && el.dataset.status !== 'playing');

            let prefix = [...played, ...playing];
            if (upcoming.length <= 1) {
                prefix = [...playing];
                upcoming = items.filter(el => el.dataset.status !== 'playing');
            }

            for (let i = upcoming.length - 1; i > 0; i--) {
                const j = Math.floor(Math.random() * (i + 1));
                [upcoming[i], upcoming[j]] = [upcoming[j], upcoming[i]];
            }

            const reordered = [...prefix, ...upcoming];
            const ids = reordered.map(el => Number(el.dataset.id)).filter(id => id > 0);

            const response = await fetch('/queue/reorder', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ids })
            });
            if (!response.ok) throw new Error('No se pudo mezclar la lista');
            app.showActionToast('Canciones siguientes mezcladas');
        } catch (err) {
            console.error(err);
            app.showActionToast('Error al mezclar la cola', true);
        } finally {
            if (button) button.disabled = false;
        }
    };

    // Re-sync search cards after HTMX swaps search results
    document.addEventListener('htmx:afterSwap', event => {
        if (event.target && event.target.id === 'search-results') {
            app.syncSearchResultsQueueStatus();
        }
    });

    // Preserve drag handles whenever #queue-container is swapped or mutated while reordering is active
    window.addEventListener('htmx:oobAfterSwap', () => {
        if (app.isReordering && !app.draggedItem) {
            app.enableDragHandles();
        }
    });

    document.addEventListener('htmx:afterSettle', () => {
        if (app.isReordering && !app.draggedItem) {
            app.enableDragHandles();
        }
    });

    const observeQueueContainer = () => {
        const list = getQueueContainer();
        if (!list) return;
        const observer = new MutationObserver(() => {
            if (app.isReordering && !app.draggedItem) {
                app.enableDragHandles();
            }
        });
        observer.observe(list, { childList: true });
    };

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', observeQueueContainer);
    } else {
        observeQueueContainer();
    }

    // Prevent clicking a queue item from triggering /room/play-item while in reorder mode
    document.addEventListener('htmx:beforeRequest', event => {
        if (!app.isReordering) return;
        const elt = event.detail?.elt;
        if (elt && elt.closest('#queue-container') && elt.getAttribute('hx-post') === '/room/play-item') {
            event.preventDefault();
        }
    });

    // Show auto-hiding toast when moving an item to play next in the queue
    document.addEventListener('htmx:afterRequest', event => {
        const elt = event.detail?.elt;
        if (!elt || !event.detail?.successful) return;
        const postUrl = elt.getAttribute('hx-post');
        if (postUrl === '/room/queue/move-next') {
            app.showActionToast('Canción movida para reproducir a continuación');
        }
    });

})(window.MusicApp);
