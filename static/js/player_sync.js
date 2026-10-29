// Alpine.js Bridge Controller — Host-Authoritative Master Clock, Engine Sync & Remote Follower
window.MusicApp = window.MusicApp || {};

function playerSync() {
    return {
        player: null,
        playerReady: false,
        isHost: false,
        clientId: '',
        forceHostParam: false,
        currentTrackId: '',
        currentQueueEntryId: 0,
        status: 'PAUSED',
        engine: 'youtube',
        pauseOffsetSec: 0,
        durationSec: 0,
        lastKnownHostTimeSec: 0,
        lastSyncSeq: -1,
        remoteEndedTrackId: '',
        driftThresholdSec: 1.5,
        syncTimer: null,
        suppressHostEventUntil: 0,

        init() {
            window.MusicApp.syncBridge = this;
            this.clientId = (window.crypto && typeof window.crypto.randomUUID === 'function')
                ? window.crypto.randomUUID()
                : ('tab-' + Math.random().toString(36).slice(2) + '-' + Date.now());
            this.resolveClientRole();
            this.bootYouTubeAPI();

            window.addEventListener('pagehide', () => {
                if (this.isHost && navigator.sendBeacon) {
                    navigator.sendBeacon('/room/claim-host', JSON.stringify({
                        clientId: this.clientId,
                        release: true
                    }));
                }
            });

            // Listen for HTMX OOB swaps on #player-sync-trigger
            window.addEventListener('htmx:oobAfterSwap', (event) => {
                const target = event.detail?.target;
                if (!target || target.id === 'player-sync-trigger' || target.id === 'queue-container') {
                    this.syncFromBeacon();
                }
            });

            // Periodic loop (every 1.5s):
            // - Host: reports its authoritative clock, engine & state to the server, plus a watchdog for stuck ENDED state
            // - Remote: checks local drift against the Host's last reported timestamp
            this.syncTimer = setInterval(() => {
                if (this.isHost) {
                    // Keep the single-host lease alive even when paused or before player is ready
                    this.heartbeatHostLease();
                }

                if (!this.playerReady || !this.currentTrackId) return;

                const activeEngine = window.MusicApp.currentPlayerEngine || 'youtube';
                if (this.isHost) {
                    if (activeEngine === 'youtube' && typeof this.player?.getPlayerState === 'function') {
                        const ytState = this.player.getPlayerState();
                        if (ytState === YT.PlayerState.PLAYING) {
                            this.reportHostState('PLAYING');
                        } else if (ytState === YT.PlayerState.ENDED) {
                            // Watchdog: if the player is still stuck on the Replay/Restart screen, retry advancing
                            this.notifyTrackEnded();
                        } else if (ytState === YT.PlayerState.PAUSED) {
                            const cur = typeof this.player.getCurrentTime === 'function' ? (this.player.getCurrentTime() || 0) : 0;
                            const dur = typeof this.player.getDuration === 'function' ? (this.player.getDuration() || 0) : 0;
                            if (dur > 0 && cur >= dur - 0.8 && Date.now() >= this.suppressHostEventUntil) {
                                // Watchdog: YouTube sometimes pauses ~0.5s before the end screen without firing ENDED
                                this.notifyTrackEnded();
                            } else if (document.hidden && this.status === 'PLAYING') {
                                // Prevent browser/DevTools mobile emulation from suspending playback when tabbing out
                                this.suppressHostEventUntil = Date.now() + 1000;
                                this.player.playVideo();
                            }
                        }
                    }
                } else if (activeEngine === 'youtube' && this.status === 'PLAYING') {
                    this.correctDrift(false);
                }
            }, 1500);

            // Resume immediately if browser/mobile emulator tried to pause video on tab switch
            document.addEventListener('visibilitychange', () => {
                if (!this.isHost || !this.playerReady || !this.player || !this.currentTrackId) return;
                const activeEngine = window.MusicApp.currentPlayerEngine || 'youtube';
                if (activeEngine === 'youtube' && this.status === 'PLAYING') {
                    this.suppressHostEventUntil = Date.now() + 1200;
                    if (typeof this.player.getPlayerState === 'function' && this.player.getPlayerState() !== YT.PlayerState.PLAYING) {
                        this.player.playVideo();
                    }
                }
            });
        },

        destroy() {
            if (this.syncTimer) {
                clearInterval(this.syncTimer);
            }
        },

        // Host role is strictly locked to a SINGLE active window on localhost / 127.0.0.1.
        // LAN devices (e.g. 192.168.x.x) and any 2nd window opened on localhost automatically become Remote.
        resolveClientRole() {
            const host = window.location.hostname;
            const isLocalhost = host === 'localhost' || host === '127.0.0.1' || host === '::1' || host === '[::1]';
            const params = new URLSearchParams(window.location.search);
            const modeParam = params.get('mode');

            if (!isLocalhost || modeParam === 'remote' || modeParam === 'client') {
                this.isHost = false;
                if (window.MusicApp.updateEngineUI) {
                    window.MusicApp.updateEngineUI();
                }
                return;
            }

            this.forceHostParam = modeParam === 'host';
            this.heartbeatHostLease();
        },

        heartbeatHostLease() {
            fetch('/room/claim-host', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    clientId: this.clientId,
                    forceHost: this.forceHostParam
                })
            })
                .then(res => res.ok ? res.json() : { isHost: false })
                .then(data => {
                    const granted = Boolean(data && data.isHost);
                    if (this.isHost !== granted) {
                        this.isHost = granted;
                        this.applyRoleAudioPolicy();
                        if (window.MusicApp.updateEngineUI) {
                            window.MusicApp.updateEngineUI();
                        }
                        this.syncFromBeacon();
                    }
                })
                .catch(() => {});
        },

        applyRoleAudioPolicy() {
            if (!this.playerReady || !this.player) return;

            if (this.isHost) {
                if (typeof this.player.unMute === 'function') {
                    this.player.unMute();
                }
            } else {
                if (typeof this.player.mute === 'function') {
                    this.player.mute();
                }
            }
        },

        bootYouTubeAPI() {
            if (window.YT && window.YT.Player) {
                this.mountYouTubePlayer();
                return;
            }

            const previousReady = window.onYouTubeIframeAPIReady;
            window.onYouTubeIframeAPIReady = () => {
                if (typeof previousReady === 'function') {
                    previousReady();
                }
                this.mountYouTubePlayer();
            };

            if (!document.querySelector('script[src*="youtube.com/iframe_api"]')) {
                const ytScript = document.createElement('script');
                ytScript.src = 'https://www.youtube.com/iframe_api';
                document.head.appendChild(ytScript);
            }
        },

        mountYouTubePlayer() {
            const playerEl = document.getElementById('player');
            if (!playerEl || this.player) return;

            this.player = new YT.Player('player', {
                height: '100%',
                width: '100%',
                playerVars: {
                    origin: window.location.origin,
                    enablejsapi: 1,
                    autoplay: 1,
                    disablekb: 0,
                    rel: 0
                },
                events: {
                    onReady: () => {
                        this.playerReady = true;
                        window.MusicApp.player = this.player;
                        this.applyRoleAudioPolicy();
                        this.syncFromBeacon();
                    },
                    onStateChange: (event) => this.handlePlayerStateChange(event),
                    onError: (event) => this.handlePlayerError(event)
                }
            });
        },

        // Synchronize player from #player-sync-trigger OOB beacon
        syncFromBeacon() {
            const beacon = document.getElementById('player-sync-trigger');
            if (!beacon) return;

            const nextTrackId = beacon.dataset.trackId || '';
            const nextQueueEntryId = Number(beacon.dataset.queueEntryId || '0');
            const nextTitle = beacon.dataset.trackTitle || '';
            const nextStatus = beacon.dataset.status || 'PAUSED';
            const nextEngine = beacon.dataset.engine || 'youtube';
            const nextPauseOffset = parseFloat(beacon.dataset.pauseOffset || '0');
            const nextDuration = parseFloat(beacon.dataset.duration || '0');
            const nextSyncSeq = parseInt(beacon.dataset.syncSeq || '0', 10);

            const trackChanged = nextTrackId !== this.currentTrackId;
            const engineChanged = nextEngine !== this.engine;
            const seqChanged = nextSyncSeq !== this.lastSyncSeq;

            if (trackChanged) {
                this.remoteEndedTrackId = '';
                this.lastKnownHostTimeSec = 0;
                if (window.MusicApp.getTrackFailures) {
                    window.MusicApp.getTrackFailures(nextTrackId);
                }
            }

            this.currentTrackId = nextTrackId;
            this.currentQueueEntryId = nextQueueEntryId;
            this.status = nextStatus;
            this.engine = nextEngine;
            this.pauseOffsetSec = nextPauseOffset;
            this.durationSec = nextDuration;
            this.lastSyncSeq = nextSyncSeq;

            if (nextTrackId) {
                window.MusicApp.currentQueueItem = {
                    id: nextQueueEntryId,
                    videoId: nextTrackId,
                    title: nextTitle
                };
            } else {
                window.MusicApp.currentQueueItem = null;
            }

            if (window.MusicApp.syncSearchResultsQueueStatus) {
                window.MusicApp.syncSearchResultsQueueStatus();
            }

            // Force Remote clients to use the exact same player engine as the Host
            if (!this.isHost && window.MusicApp.applyEngineFromRoom) {
                window.MusicApp.applyEngineFromRoom(nextEngine);
            }

            const activeEngine = window.MusicApp.currentPlayerEngine || 'youtube';
            if (activeEngine === 'invidious') {
                if ((trackChanged || engineChanged) && window.MusicApp.loadSongInPlayer) {
                    window.MusicApp.loadSongInPlayer(window.MusicApp.currentQueueItem, this.calculateExpectedTimeSec(), this.isHost);
                }
                return;
            }

            if (!this.playerReady || !this.player) return;

            this.applyRoleAudioPolicy();

            if (!this.currentTrackId) {
                if (typeof this.player.stopVideo === 'function') {
                    this.player.stopVideo();
                }
                return;
            }

            const expectedSec = this.calculateExpectedTimeSec();

            // 1. Track changed (or switched back to YouTube): load track at Host's position
            if (trackChanged || (!this.isHost && engineChanged)) {
                this.suppressHostEventUntil = Date.now() + 1200;
                const startSec = Math.max(0, Math.floor(expectedSec));
                if (this.status === 'PLAYING') {
                    this.player.loadVideoById({
                        videoId: this.currentTrackId,
                        startSeconds: startSec
                    });
                } else {
                    this.player.cueVideoById({
                        videoId: this.currentTrackId,
                        startSeconds: startSec
                    });
                }
                return;
            }

            // 2. Host Mode: Host is the authoritative clock.
            // It only reacts when an explicit server command (seqChanged, e.g. someone clicked HTMX Play/Pause or loop restarted) arrives.
            if (this.isHost) {
                if (seqChanged && typeof this.player.getPlayerState === 'function') {
                    const state = this.player.getPlayerState();
                    this.suppressHostEventUntil = Date.now() + 800;
                    if (this.status === 'PLAYING' && state === YT.PlayerState.ENDED) {
                        this.player.seekTo(0, true);
                        this.player.playVideo();
                    } else if (this.status === 'PLAYING' && state !== YT.PlayerState.PLAYING && state !== YT.PlayerState.BUFFERING) {
                        this.player.playVideo();
                    } else if (this.status === 'PAUSED' && state === YT.PlayerState.PLAYING) {
                        this.player.pauseVideo();
                    }
                }
                return;
            }

            // 3. Remote Mode: If Remote already reached end of this track (or Host is within 1.5s of end),
            // stay paused at the end to prevent an infinite play/pause seek loop while waiting for Host.
            if (this.remoteEndedTrackId === this.currentTrackId || (this.durationSec > 0 && expectedSec >= this.durationSec - 1.5)) {
                if (typeof this.player.getPlayerState === 'function' && this.player.getPlayerState() === YT.PlayerState.PLAYING) {
                    this.player.pauseVideo();
                }
                return;
            }

            this.correctDrift(true);

            if (typeof this.player.getPlayerState === 'function') {
                const state = this.player.getPlayerState();
                if (this.status === 'PLAYING' && state !== YT.PlayerState.PLAYING && state !== YT.PlayerState.BUFFERING) {
                    this.player.playVideo();
                } else if (this.status === 'PAUSED' && state === YT.PlayerState.PLAYING) {
                    this.player.pauseVideo();
                }
            }
        },

        // Strictly return the Host's last reported position (pauseOffsetSec) — never extrapolate ahead of the Host!
        calculateExpectedTimeSec() {
            return Math.max(0, this.pauseOffsetSec);
        },

        // Executed ONLY on Remote clients so they stay locked to the Host clock
        correctDrift(forceWhenPaused) {
            if (this.isHost || !this.player || typeof this.player.getCurrentTime !== 'function') return;
            if (this.remoteEndedTrackId === this.currentTrackId) return;

            const expectedSec = this.calculateExpectedTimeSec();
            if (this.durationSec > 0 && expectedSec >= this.durationSec - 1.5) return;

            const actualSec = this.player.getCurrentTime() || 0;
            const drift = Math.abs(actualSec - expectedSec);

            if (drift > this.driftThresholdSec || (forceWhenPaused && this.status === 'PAUSED' && drift > 0.5)) {
                this.player.seekTo(expectedSec, true);
            }
        },

        handlePlayerStateChange(event) {
            if (!this.currentTrackId) return;

            if (this.isHost) {
                if (event.data === YT.PlayerState.ENDED) {
                    this.notifyTrackEnded();
                    return;
                }

                if (Date.now() < this.suppressHostEventUntil) {
                    return;
                }

                if (event.data === YT.PlayerState.PLAYING) {
                    this.applyRoleAudioPolicy();
                    this.reportHostState('PLAYING');
                } else if (event.data === YT.PlayerState.PAUSED) {
                    // If the tab/window is hidden (or DevTools mobile emulation auto-paused on blur),
                    // do NOT pause the room—resume playback immediately!
                    if (document.hidden && this.status === 'PLAYING') {
                        this.suppressHostEventUntil = Date.now() + 1000;
                        this.player.playVideo();
                        return;
                    }
                    this.reportHostState('PAUSED');
                }
                return;
            }

            // Remote Mode:
            this.applyRoleAudioPolicy();
            if (event.data === YT.PlayerState.ENDED) {
                // Mark this track as ended on the Remote so it never enters a seek/play loop at 0:00
                this.remoteEndedTrackId = this.currentTrackId;
                return;
            }

            if (event.data === YT.PlayerState.PLAYING) {
                if (this.status === 'PAUSED' || this.remoteEndedTrackId === this.currentTrackId) {
                    this.player.pauseVideo();
                } else {
                    // If user scrubbed backward on Remote, unlock remoteEndedTrackId and re-sync to Host
                    this.correctDrift(false);
                }
            } else if (event.data === YT.PlayerState.PAUSED) {
                if (this.status === 'PLAYING' && this.remoteEndedTrackId !== this.currentTrackId) {
                    const expectedSec = this.calculateExpectedTimeSec();
                    if (!this.durationSec || expectedSec < this.durationSec - 1.5) {
                        this.player.playVideo();
                    }
                }
            }
        },

        reportHostState(hostStatus, overrideEngine) {
            if (!this.isHost || !this.currentTrackId) return;

            const activeEngine = overrideEngine || window.MusicApp.currentPlayerEngine || 'youtube';
            let currentTime = this.lastKnownHostTimeSec || this.pauseOffsetSec || 0;
            let duration = this.durationSec || 0;

            if (activeEngine === 'youtube' && this.player) {
                if (typeof this.player.getCurrentTime === 'function') {
                    const ytTime = this.player.getCurrentTime();
                    if (ytTime > 0) {
                        currentTime = ytTime;
                        this.lastKnownHostTimeSec = ytTime;
                    }
                }
                if (typeof this.player.getDuration === 'function') {
                    const ytDur = this.player.getDuration();
                    if (ytDur > 0) {
                        duration = ytDur;
                    }
                }
            }

            fetch('/room/host-sync', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-Room-Role': 'host',
                    'X-Client-ID': this.clientId
                },
                body: JSON.stringify({
                    trackId: this.currentTrackId,
                    status: hostStatus,
                    engine: activeEngine,
                    currentTime: currentTime,
                    duration: duration
                })
            })
                .then(res => {
                    if (res.status === 409 || res.status === 403) {
                        this.isHost = false;
                        this.applyRoleAudioPolicy();
                        if (window.MusicApp.updateEngineUI) {
                            window.MusicApp.updateEngineUI();
                        }
                    }
                })
                .catch((err) => console.error('Error reporting host state:', err));
        },

        handlePlayerError(event) {
            if (!this.isHost || !this.currentTrackId) return;

            const failures = window.MusicApp.recordEngineFailure
                ? window.MusicApp.recordEngineFailure(this.currentTrackId, 'youtube')
                : { invidious: false };

            if (window.MusicApp.currentPlayerEngine === 'youtube' && !failures.invidious) {
                console.warn('YouTube error (' + event.data + '), switching to Invidious fallback...');
                if (window.MusicApp.showActionToast) {
                    window.MusicApp.showActionToast('YouTube no pudo reproducir el video, intentando con Invidious...');
                }
                window.MusicApp.switchPlayerEngine('invidious');
            } else {
                console.warn('Both YouTube and Invidious failed for ' + this.currentTrackId + ', skipping track...');
                if (window.MusicApp.showActionToast) {
                    window.MusicApp.showActionToast('Video no disponible en YouTube ni Invidious, saltando canción...', true);
                }
                if (window.MusicApp.currentPlayerEngine !== 'youtube') {
                    window.MusicApp.switchPlayerEngine('youtube');
                }
                this.notifyTrackEnded();
            }
        },

        notifyTrackEnded() {
            if (!this.isHost) return;

            fetch('/room/track-ended', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-Room-Role': 'host',
                    'X-Client-ID': this.clientId
                },
                body: JSON.stringify({
                    trackId: this.currentTrackId,
                    id: this.currentQueueEntryId
                })
            }).catch((err) => console.error('Error notifying track_ended:', err));
        }
    };
}

window.playerSync = playerSync;

document.addEventListener('alpine:init', () => {
    if (window.Alpine) {
        window.Alpine.data('playerSync', playerSync);
    }
});
