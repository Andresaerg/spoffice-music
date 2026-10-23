// Player engine & playback management
window.MusicApp = window.MusicApp || {};

(function (app) {
    const configEl = document.querySelector('#app-config');
    app.INVIDIOUS_HOST = configEl?.dataset.invidiousHost || "https://invidious.f5.si";
    app.currentPlayerEngine = localStorage.getItem('spoffice_player_engine') || 'youtube';
    app.currentQueueItem = null;
    app.player = null;

    app.getYtWrapper = () => document.querySelector('#youtube-wrapper');
    app.getInvidiousIframe = () => document.querySelector('#invidious-iframe');
    app.getEngineYtBtn = () => document.querySelector('#engine-yt-btn');
    app.getEngineInvidiousBtn = () => document.querySelector('#engine-invidious-btn');
    app.getSkipSongBtn = () => document.querySelector('#skip-song-btn');

    app.updateEngineUI = function () {
        const engineYtBtn = app.getEngineYtBtn();
        const engineInvidiousBtn = app.getEngineInvidiousBtn();
        if (!engineYtBtn || !engineInvidiousBtn) return;

        if (app.currentPlayerEngine === 'youtube') {
            engineYtBtn.className = 'flex items-center gap-1.5 rounded-md px-3 py-1 font-semibold transition-all bg-red-600 text-white shadow cursor-pointer';
            engineInvidiousBtn.className = 'flex items-center gap-1.5 rounded-md px-3 py-1 font-semibold transition-all text-slate-400 hover:text-white cursor-pointer';
        } else {
            engineYtBtn.className = 'flex items-center gap-1.5 rounded-md px-3 py-1 font-semibold transition-all text-slate-400 hover:text-white cursor-pointer';
            engineInvidiousBtn.className = 'flex items-center gap-1.5 rounded-md px-3 py-1 font-semibold transition-all bg-indigo-600 text-white shadow cursor-pointer';
        }
    };

    app.switchPlayerEngine = function (engine) {
        if (app.currentPlayerEngine === engine) return;
        app.currentPlayerEngine = engine;
        localStorage.setItem('spoffice_player_engine', engine);
        app.updateEngineUI();
        if (app.currentQueueItem) {
            app.loadSongInPlayer(app.currentQueueItem);
        }
    };

    app.loadSongInPlayer = function (song) {
        if (!song || !song.videoId) return;

        const ytWrapper = app.getYtWrapper();
        const invidiousIframe = app.getInvidiousIframe();

        if (app.currentPlayerEngine === 'invidious') {
            if (app.player && typeof app.player.stopVideo === 'function') {
                app.player.stopVideo();
            }
            ytWrapper?.classList.add('hidden');
            if (invidiousIframe) {
                invidiousIframe.classList.remove('hidden');
                invidiousIframe.src = `${app.INVIDIOUS_HOST}/embed/${encodeURIComponent(song.videoId)}?autoplay=1&listen=0`;
            }
        } else {
            if (invidiousIframe) {
                invidiousIframe.src = '';
                invidiousIframe.classList.add('hidden');
            }
            ytWrapper?.classList.remove('hidden');
            if (app.player && typeof app.player.loadVideoById === 'function') {
                app.player.loadVideoById(song.videoId);
            }
        }
    };

    app.skipCurrentSong = async function () {
        if (!app.currentQueueItem) return;
        const skipSongBtn = app.getSkipSongBtn();
        if (skipSongBtn) skipSongBtn.disabled = true;
        try {
            const response = await fetch('/queue/finish', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ id: app.currentQueueItem.id })
            });
            if (!response.ok) throw new Error('No se pudo finalizar la canción');
            app.currentQueueItem = null;
            await app.playNextSong();
        } catch (err) {
            console.error('Error al saltar canción:', err);
        } finally {
            if (skipSongBtn) skipSongBtn.disabled = false;
        }
    };

    app.playNextSong = async function () {
        const response = await fetch('/queue/next');
        if (!response.ok) throw new Error('No se pudo obtener la siguiente canción');
        const song = await response.json();

        const nowPlayingEl = document.querySelector('#now-playing');
        const miniNowPlayingEl = document.querySelector('#mini-now-playing');

        if (!song.id) {
            app.currentQueueItem = null;
            if (nowPlayingEl) nowPlayingEl.textContent = 'No hay canciones en la lista.';
            if (miniNowPlayingEl) miniNowPlayingEl.textContent = 'Sin reproducción';
            if (app.player && typeof app.player.stopVideo === 'function') {
                app.player.stopVideo();
            }
            const invidiousIframe = app.getInvidiousIframe();
            if (invidiousIframe) invidiousIframe.src = '';
            if (app.updateQueueCount) app.updateQueueCount();
            return;
        }

        app.currentQueueItem = song;
        if (nowPlayingEl) nowPlayingEl.textContent = song.title;
        if (miniNowPlayingEl) miniNowPlayingEl.textContent = song.title;
        if (app.updateQueueCount) app.updateQueueCount();
        app.loadSongInPlayer(song);
        if (app.refreshQueue) {
            app.refreshQueue().catch(console.error);
        }
    };

    app.playQueueItem = async function (id) {
        const response = await fetch('/queue/play', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id })
        });
        if (!response.ok) throw new Error('No se pudo seleccionar la canción');

        const song = await response.json();
        app.currentQueueItem = song;
        const nowPlayingEl = document.querySelector('#now-playing');
        const miniNowPlayingEl = document.querySelector('#mini-now-playing');
        if (nowPlayingEl) nowPlayingEl.textContent = song.title;
        if (miniNowPlayingEl) miniNowPlayingEl.textContent = song.title;
        if (app.updateQueueCount) app.updateQueueCount();
        app.loadSongInPlayer(song);
        if (app.refreshQueue) {
            app.refreshQueue().catch(console.error);
        }
    };

    // YouTube IFrame API global callback
    window.onYouTubeIframeAPIReady = function () {
        app.player = new YT.Player('player', {
            height: '100%',
            width: '100%',
            playerVars: {
                origin: window.location.origin,
                enablejsapi: 1,
                autoplay: 1,
                disablekb: 0
            },
            events: {
                onReady: () => {
                    if (app.initializePlayerQueue) {
                        app.initializePlayerQueue().catch(console.error);
                    }
                },
                onStateChange: (event) => {
                    if (event.data === YT.PlayerState.ENDED && app.currentQueueItem) {
                        fetch('/queue/finish', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ id: app.currentQueueItem.id })
                        })
                            .then(response => {
                                if (!response.ok) throw new Error('No se pudo finalizar la canción');
                                app.currentQueueItem = null;
                                return app.playNextSong();
                            })
                            .catch(console.error);
                    }
                },
                onError: (event) => {
                    console.warn('YouTube bloqueó o tuvo un error con la canción (código ' + event.data + '). Cambiando automáticamente a Invidious...');
                    if (app.currentQueueItem && app.currentPlayerEngine === 'youtube') {
                        app.switchPlayerEngine('invidious');
                    } else if (app.currentQueueItem) {
                        app.skipCurrentSong().catch(console.error);
                    }
                }
            }
        });
    };

    // Invidious postMessage listener
    window.addEventListener('message', (event) => {
        if (!app.currentQueueItem || app.currentPlayerEngine !== 'invidious') return;
        if (event.data && (event.data === 'ended' || event.data.event === 'onEnded' || event.data.type === 'ended')) {
            app.skipCurrentSong().catch(console.error);
        }
    });

    // Load YouTube API script tag
    const ytScript = document.createElement("script");
    ytScript.src = "https://www.youtube.com/iframe_api";
    document.head.appendChild(ytScript);

})(window.MusicApp);
