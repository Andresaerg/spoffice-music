// Queue management, rendering, reordering, and synchronization
window.MusicApp = window.MusicApp || {};

(function (app) {
    app.isReordering = false;
    app.lastQueueEntries = [];
    app.draggedItem = null;

    app.refreshQueue = async function () {
        const response = await fetch('/queue');
        if (!response.ok) throw new Error('No se pudo cargar la lista');
        app.renderQueue(await response.json());
    };

    app.initializePlayerQueue = async function () {
        const response = await fetch('/queue');
        if (!response.ok) throw new Error('No se pudo cargar la lista');
        const entries = await response.json();

        if (entries.length === 0) {
            const randomResponse = await fetch('/queue/random?if_empty=true', { method: 'POST' });
            if (!randomResponse.ok) throw new Error('No se pudieron cargar canciones aleatorias');
        }

        await app.refreshQueue();
        await app.playNextSong();
    };

    app.addRandomSongs = async function () {
        const button = document.querySelector('#random-queue');
        const status = document.querySelector('#random-queue-status');
        if (!button || !status) return;

        button.disabled = true;
        status.textContent = 'Seleccionando canciones…';

        try {
            const response = await fetch('/queue/random', { method: 'POST' });
            if (!response.ok) throw new Error('No se pudieron agregar canciones aleatorias');
            const result = await response.json();
            await app.refreshQueue();

            status.textContent = result.added > 0
                ? `Se agregaron ${result.added} canciones aleatorias al final de la cola.`
                : 'No hay canciones nuevas disponibles para agregar.';
        } catch (error) {
            status.textContent = 'No se pudieron agregar canciones aleatorias.';
            throw error;
        } finally {
            button.disabled = false;
        }
    };

    app.updateQueueCount = function (entries) {
        const countEl = document.querySelector('#queue-count');
        if (!countEl) return;
        const list = entries || app.lastQueueEntries || [];
        const total = list.length;
        if (total === 0) {
            countEl.textContent = '0/0 canciones';
            return;
        }

        let current = 0;
        if (app.currentQueueItem && app.currentQueueItem.id) {
            const idx = list.findIndex(e => Number(e.id) === Number(app.currentQueueItem.id));
            if (idx >= 0) {
                current = idx + 1;
            } else if (app.currentQueueItem.position) {
                current = app.currentQueueItem.position;
            }
        }

        if (current === 0) {
            const idx = list.findIndex(e => e.status === 'playing');
            if (idx >= 0) {
                current = idx + 1;
            }
        }

        countEl.textContent = `${current}/${total} canciones`;
    };

    app.syncSearchResultsQueueStatus = function (entries) {
        const list = entries || app.lastQueueEntries || [];
        const inQueueIds = new Set(list.map(e => String(e.videoId || e.video_id || '')));

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
                    <button disabled type="button" class="cursor-default inline-flex items-center gap-1.5 rounded border border-white/10 bg-white/5 px-3 py-2 text-xs font-semibold text-slate-400">
                        <svg class="size-3.5 text-slate-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg>
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
                        class="cursor-pointer rounded bg-red-600 px-3 py-2 text-sm font-semibold text-white hover:bg-red-700 transition-colors"
                        title="Agregar al final de la cola"
                    >
                        Agregar a la cola
                    </button>
                    <button
                        type="submit"
                        hx-post="/add-to-queue?next=true"
                        hx-include="closest li"
                        hx-target="closest form"
                        hx-swap="outerHTML"
                        class="cursor-pointer inline-flex items-center gap-1.5 rounded border border-white/20 bg-white/10 px-3 py-2 text-sm font-semibold text-white hover:bg-white/20 hover:border-white/30 transition-colors"
                        title="Reproducir inmediatamente después de la canción actual"
                    >
                        <svg class="size-4 shrink-0 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 5l7 7-7 7M5 5l7 7-7 7" />
                        </svg>
                        <span>Reproducir siguiente</span>
                    </button>
                `;
                if (window.htmx) {
                    htmx.process(form);
                }
            }
        });
    };

    app.renderQueue = function (entries) {
        app.lastQueueEntries = entries;
        app.updateQueueCount(entries);
        app.syncSearchResultsQueueStatus(entries);
        const list = document.querySelector('#queue-list');
        if (!list) return;

        list.replaceChildren();

        if (entries.length === 0) {
            const empty = document.createElement('li');
            empty.className = 'px-4 py-6 text-sm text-slate-400';
            empty.textContent = 'No hay canciones en la lista.';
            list.appendChild(empty);

            app.currentQueueItem = null;
            const nowPlayingEl = document.querySelector('#now-playing');
            if (nowPlayingEl) nowPlayingEl.textContent = 'No hay canciones en la lista.';
            if (app.player && typeof app.player.stopVideo === 'function') {
                app.player.stopVideo();
            }
            return;
        }

        for (const entry of entries) {
            const item = document.createElement('li');
            item.dataset.id = entry.id;
            item.className = 'group flex items-center justify-between gap-2 px-3 py-2 hover:bg-white/5 transition-colors';
            const isPlaying = (app.currentQueueItem && Number(entry.id) === Number(app.currentQueueItem.id)) || entry.status === 'playing';
            if (isPlaying) {
                item.classList.add('bg-white/10');
            }

            if (app.isReordering) {
                item.draggable = true;
                item.classList.add('cursor-grab', 'active:cursor-grabbing');

                const handle = document.createElement('span');
                handle.className = 'select-none px-1 text-slate-500 hover:text-white shrink-0 font-bold';
                handle.textContent = '⋮⋮';
                handle.title = 'Arrastra para mover';
                item.appendChild(handle);

                item.addEventListener('dragstart', (e) => {
                    app.draggedItem = item;
                    e.dataTransfer.effectAllowed = 'move';
                    e.dataTransfer.setData('text/plain', entry.id);
                    setTimeout(() => item.classList.add('opacity-40', 'bg-white/5'), 0);
                });

                item.addEventListener('dragend', () => {
                    item.classList.remove('opacity-40', 'bg-white/5');
                    app.draggedItem = null;
                    app.saveNewOrder().catch(console.error);
                });

                item.addEventListener('dragover', (e) => {
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
                });
            }

            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'flex min-w-0 flex-1 items-center gap-3 text-left cursor-pointer';
            if (isPlaying) {
                button.setAttribute('aria-current', 'true');
            }

            const image = document.createElement('img');
            image.className = 'h-12 w-16 shrink-0 rounded object-cover';
            image.src = entry.thumbnail;
            image.alt = '';

            const text = document.createElement('span');
            text.className = 'min-w-0 flex-1 overflow-hidden';
            const title = document.createElement('span');
            title.className = 'block truncate text-sm font-medium';
            title.textContent = entry.title;
            title.title = entry.title;
            const status = document.createElement('span');
            status.className = 'mt-1 block text-xs text-slate-400';
            status.textContent = isPlaying ? 'Reproduciendo' : entry.status === 'played' ? 'Reproducida' : 'En espera';
            text.append(title, status);
            button.append(image, text);
            button.addEventListener('click', () => {
                if (app.isReordering) return;
                app.playQueueItem(entry.id).catch(console.error);
            });
            item.appendChild(button);

            const deleteBtn = document.createElement('button');
            deleteBtn.type = 'button';
            deleteBtn.className = 'shrink-0 rounded p-1.5 text-slate-400 hover:bg-white/10 hover:text-red-400 transition-colors cursor-pointer';
            deleteBtn.title = 'Eliminar de la lista';
            deleteBtn.setAttribute('aria-label', `Eliminar ${entry.title}`);
            deleteBtn.innerHTML = `
                <svg class="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
            `;
            deleteBtn.addEventListener('click', (e) => {
                e.stopPropagation();
                app.removeQueueItem(entry.id).catch(console.error);
            });
            item.appendChild(deleteBtn);

            list.appendChild(item);
        }
    };

    app.removeQueueItem = async function (id) {
        const response = await fetch('/queue/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id })
        });
        if (!response.ok) throw new Error('No se pudo eliminar la canción');

        const wasPlaying = app.currentQueueItem && app.currentQueueItem.id === id;
        if (wasPlaying) {
            app.currentQueueItem = null;
        }
        await app.refreshQueue();
        if (wasPlaying) {
            await app.playNextSong();
        }
    };

    app.saveNewOrder = async function () {
        const list = document.querySelector('#queue-list');
        if (!list) return;

        const ids = Array.from(list.children)
            .map(el => Number(el.dataset.id))
            .filter(id => !isNaN(id) && id > 0);

        if (ids.length === 0) return;

        const entryMap = new Map(app.lastQueueEntries.map(e => [e.id, e]));
        app.lastQueueEntries = ids.map(id => entryMap.get(id)).filter(Boolean);

        const response = await fetch('/queue/reorder', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ids })
        });
        if (!response.ok) throw new Error('No se pudo guardar el nuevo orden');
    };

    app.toggleReorderMode = async function () {
        app.isReordering = !app.isReordering;
        const btn = document.querySelector('#reorder-queue');
        if (!btn) return;

        if (app.isReordering) {
            btn.textContent = 'Listo';
            btn.classList.add('bg-white/20', 'text-white', 'border-white/40', 'font-semibold');
            app.renderQueue(app.lastQueueEntries);
        } else {
            btn.textContent = 'Reordenar';
            btn.classList.remove('bg-white/20', 'text-white', 'border-white/40', 'font-semibold');
            await app.refreshQueue();
        }
    };

    app.shuffleQueue = async function () {
        if (app.lastQueueEntries.length <= 1) return;
        const button = document.querySelector('#shuffle-queue');
        if (button) button.disabled = true;

        try {
            const playing = app.lastQueueEntries.find(e => e.status === 'playing');
            const others = app.lastQueueEntries.filter(e => e.status !== 'playing');

            for (let i = others.length - 1; i > 0; i--) {
                const j = Math.floor(Math.random() * (i + 1));
                [others[i], others[j]] = [others[j], others[i]];
            }

            const reordered = playing ? [playing, ...others] : others;
            const ids = reordered.map(e => e.id);

            const response = await fetch('/queue/reorder', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ids })
            });
            if (!response.ok) throw new Error('No se pudo mezclar la lista');

            await app.refreshQueue();
        } catch (err) {
            console.error(err);
        } finally {
            if (button) button.disabled = false;
        }
    };

    // Listen for queue events pushed from server (SSE)
    const queueEvents = new EventSource('/queue/events');
    queueEvents.addEventListener('queue-updated', () => {
        if (!app.isReordering) {
            app.refreshQueue().catch(console.error);
        }
    });

    // Custom event triggered after adding YouTube link via modal
    document.addEventListener('youtube-link-added', event => {
        if (event.detail?.added && !app.currentQueueItem) {
            app.playNextSong().catch(console.error);
        }
    });

    // Re-sync cards after HTMX swaps search results
    document.addEventListener('htmx:afterSwap', event => {
        if (event.target && event.target.id === 'search-results') {
            app.syncSearchResultsQueueStatus();
        }
    });

})(window.MusicApp);
