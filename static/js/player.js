// Player engine & playback management integrated with Host-Authoritative playerSync bridge
window.MusicApp = window.MusicApp || {};

(function (app) {
    const configEl = document.querySelector('#app-config');
    app.INVIDIOUS_HOST = configEl?.dataset.invidiousHost || "https://invidious.f5.si";
    app.currentPlayerEngine = 'youtube';
    app.currentQueueItem = null;
    app.player = null;
    app.trackFailures = {
        videoId: '',
        youtube: false,
        invidious: false
    };

    app.getTrackFailures = function (videoId) {
        if (!videoId || app.trackFailures.videoId !== videoId) {
            app.trackFailures = {
                videoId: videoId || '',
                youtube: false,
                invidious: false
            };
        }
        return app.trackFailures;
    };

    app.recordEngineFailure = function (videoId, engine) {
        const state = app.getTrackFailures(videoId);
        if (engine === 'youtube') state.youtube = true;
        if (engine === 'invidious') state.invidious = true;
        return state;
    };

    app.verifyInvidiousPlayback = async function (videoId) {
        if (!videoId || !app.syncBridge || !app.syncBridge.isHost) return;
        try {
            const res = await fetch(`/room/invidious-check?video_id=${encodeURIComponent(videoId)}`);
            if (!res.ok) {
                app.handleInvidiousFailure(videoId);
                return;
            }
            const data = await res.json();
            if (app.currentPlayerEngine === 'invidious' && app.currentQueueItem?.videoId === videoId && !data.playable) {
                app.handleInvidiousFailure(videoId);
            }
        } catch (err) {
            console.warn('Invidious check error:', err);
            if (app.currentPlayerEngine === 'invidious' && app.currentQueueItem?.videoId === videoId) {
                app.handleInvidiousFailure(videoId);
            }
        }
    };

    app.handleInvidiousFailure = function (videoId) {
        if (!app.syncBridge || !app.syncBridge.isHost) return;
        if (app.currentPlayerEngine !== 'invidious') return;

        const failures = app.recordEngineFailure(videoId, 'invidious');
        console.warn('Invidious failed to play (' + videoId + '), switching back to YouTube...');

        if (!failures.youtube) {
            if (app.showActionToast) {
                app.showActionToast('Invidious falló, cambiando automáticamente a YouTube...');
            }
            app.switchPlayerEngine('youtube');
        } else {
            if (app.showActionToast) {
                app.showActionToast('Video no disponible en YouTube ni Invidious, saltando canción...', true);
            }
            app.switchPlayerEngine('youtube');
            app.syncBridge.notifyTrackEnded();
        }
    };

    app.getYtWrapper = () => document.querySelector('#youtube-wrapper');
    app.getInvidiousIframe = () => document.querySelector('#invidious-iframe');
    app.getEngineYtBtn = () => document.querySelector('#engine-yt-btn');
    app.getEngineInvidiousBtn = () => document.querySelector('#engine-invidious-btn');
    app.getSkipSongBtn = () => document.querySelector('#skip-song-btn');

    app.updateEngineUI = function () {
        const engineYtBtn = app.getEngineYtBtn();
        const engineInvidiousBtn = app.getEngineInvidiousBtn();
        if (!engineYtBtn || !engineInvidiousBtn) return;

        const isHost = app.syncBridge ? app.syncBridge.isHost : false;
        engineYtBtn.disabled = !isHost;
        engineInvidiousBtn.disabled = !isHost;

        const lockClass = isHost ? 'cursor-pointer' : 'cursor-not-allowed opacity-60';

        if (app.currentPlayerEngine === 'youtube') {
            engineYtBtn.className = `flex items-center gap-1.5 rounded-full px-3 py-1 font-semibold transition-all bg-red-600 text-white shadow ${lockClass}`;
            engineInvidiousBtn.className = `flex items-center gap-1.5 rounded-full px-3 py-1 font-semibold transition-all text-slate-400 hover:text-white ${lockClass}`;
        } else {
            engineYtBtn.className = `flex items-center gap-1.5 rounded-full px-3 py-1 font-semibold transition-all text-slate-400 hover:text-white ${lockClass}`;
            engineInvidiousBtn.className = `flex items-center gap-1.5 rounded-full px-3 py-1 font-semibold transition-all bg-indigo-600 text-white shadow ${lockClass}`;
        }
    };

    // Called on Remote clients when the Host changes the room's player engine
    app.applyEngineFromRoom = function (roomEngine) {
        if (!roomEngine || app.currentPlayerEngine === roomEngine) return;
        app.currentPlayerEngine = roomEngine;
        app.updateEngineUI();

        const ytWrapper = app.getYtWrapper();
        const invidiousIframe = app.getInvidiousIframe();

        if (roomEngine === 'invidious') {
            if (app.player && typeof app.player.stopVideo === 'function') {
                app.player.stopVideo();
            }
            ytWrapper?.classList.add('hidden');
            invidiousIframe?.classList.remove('hidden');
            if (app.currentQueueItem) {
                const startSec = app.syncBridge ? Math.floor(app.syncBridge.calculateExpectedTimeSec()) : 0;
                app.loadSongInPlayer(app.currentQueueItem, startSec, false);
            }
        } else {
            if (invidiousIframe) {
                invidiousIframe.src = '';
                invidiousIframe.classList.add('hidden');
            }
            ytWrapper?.classList.remove('hidden');
        }
    };

    // Host-only action to switch the room's active player engine
    app.switchPlayerEngine = function (engine) {
        if (app.syncBridge && !app.syncBridge.isHost) {
            return;
        }
        if (app.currentPlayerEngine === engine) return;

        // Capture Host's exact YouTube timestamp before stopping the YouTube player
        if (app.currentPlayerEngine === 'youtube' && app.syncBridge && app.player && typeof app.player.getCurrentTime === 'function') {
            const currentSec = app.player.getCurrentTime() || 0;
            if (currentSec > 0) {
                app.syncBridge.lastKnownHostTimeSec = currentSec;
                app.syncBridge.pauseOffsetSec = currentSec;
            }
        }

        app.currentPlayerEngine = engine;
        app.updateEngineUI();

        const ytWrapper = app.getYtWrapper();
        const invidiousIframe = app.getInvidiousIframe();

        if (engine === 'invidious') {
            if (app.player && typeof app.player.stopVideo === 'function') {
                app.player.stopVideo();
            }
            ytWrapper?.classList.add('hidden');
            invidiousIframe?.classList.remove('hidden');

            const startSec = app.syncBridge ? Math.floor(app.syncBridge.lastKnownHostTimeSec || app.syncBridge.pauseOffsetSec || 0) : 0;
            if (app.currentQueueItem) {
                app.loadSongInPlayer(app.currentQueueItem, startSec, true);
            }
            if (app.syncBridge) {
                app.syncBridge.reportHostState('PAUSED', 'invidious');
            }
        } else {
            if (invidiousIframe) {
                invidiousIframe.src = '';
                invidiousIframe.classList.add('hidden');
            }
            ytWrapper?.classList.remove('hidden');
            if (app.syncBridge && app.currentQueueItem) {
                const resumeSec = Math.max(0, Math.floor(app.syncBridge.lastKnownHostTimeSec || app.syncBridge.pauseOffsetSec || 0));
                app.syncBridge.suppressHostEventUntil = Date.now() + 1200;
                if (app.player && typeof app.player.loadVideoById === 'function') {
                    app.player.loadVideoById({
                        videoId: app.currentQueueItem.videoId,
                        startSeconds: resumeSec
                    });
                }
                app.syncBridge.reportHostState('PLAYING', 'youtube');
            }
        }
    };

    app.loadSongInPlayer = function (song, startSeconds = 0, isHost = true) {
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
                const startParam = startSeconds > 1 ? `&t=${Math.floor(startSeconds)}` : '';
                const volumeParam = isHost ? '' : '&volume=0';
                invidiousIframe.src = `${app.INVIDIOUS_HOST}/embed/${encodeURIComponent(song.videoId)}?autoplay=1&listen=0&local=true${startParam}${volumeParam}`;
                if (isHost) {
                    app.verifyInvidiousPlayback(song.videoId);
                }
            }
        } else {
            if (invidiousIframe) {
                invidiousIframe.src = '';
                invidiousIframe.classList.add('hidden');
            }
            ytWrapper?.classList.remove('hidden');
            if (app.player && typeof app.player.loadVideoById === 'function') {
                app.player.loadVideoById({
                    videoId: song.videoId,
                    startSeconds: Math.max(0, Math.floor(startSeconds))
                });
            }
        }
    };

    app.skipCurrentSong = async function () {
        const response = await fetch('/room/skip', { method: 'POST' });
        if (!response.ok) throw new Error('No se pudo saltar la canción');
    };

    app.playNextSong = async function () {
        const response = await fetch('/room/play', { method: 'POST' });
        if (!response.ok) throw new Error('No se pudo iniciar la reproducción');
    };

    app.playQueueItem = async function (id) {
        const response = await fetch('/room/play-item', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id })
        });
        if (!response.ok) throw new Error('No se pudo seleccionar la canción');
    };

    // Invidious postMessage listener for Host Mode track completion or playback error
    window.addEventListener('message', (event) => {
        if (!app.currentQueueItem || app.currentPlayerEngine !== 'invidious') return;
        const isHost = app.syncBridge ? app.syncBridge.isHost : false;
        if (!isHost) return;
        if (event.data) {
            if (event.data === 'ended' || event.data.event === 'onEnded' || event.data.type === 'ended') {
                if (app.syncBridge) {
                    app.syncBridge.notifyTrackEnded();
                }
            } else if (event.data === 'error' || event.data.event === 'onError' || event.data.type === 'error') {
                app.handleInvidiousFailure(app.currentQueueItem.videoId);
            }
        }
    });

})(window.MusicApp);
