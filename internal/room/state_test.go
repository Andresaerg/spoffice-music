package room

import (
	"testing"
	"time"
)

func TestRoomStateLifecycleAndProgress(t *testing.T) {
	state := NewRoomState()

	if err := state.Play(); err != ErrEmptyQueue {
		t.Fatalf("expected ErrEmptyQueue on empty state, got %v", err)
	}

	track1 := Track{
		ID:           "1",
		VideoID:      "dQw4w9WgXcQ",
		Title:        "Never Gonna Give You Up",
		Author:       "Rick Astley",
		Duration:     3 * time.Minute,
		ThumbnailURL: "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg",
	}
	track2 := Track{
		ID:           "2",
		VideoID:      "9bZkp7q19f0",
		Title:        "Gangnam Style",
		Author:       "PSY",
		Duration:     4 * time.Minute,
		ThumbnailURL: "https://i.ytimg.com/vi/9bZkp7q19f0/hqdefault.jpg",
	}
	track3 := Track{
		ID:           "3",
		VideoID:      "kJQP7kiw5Fk",
		Title:        "Despacito",
		Author:       "Luis Fonsi",
		Duration:     4 * time.Minute,
		ThumbnailURL: "https://i.ytimg.com/vi/kJQP7kiw5Fk/hqdefault.jpg",
	}

	// Auto-starts first track when room is idle
	state.AddToQueue(track1)
	state.AddToQueue(track2)
	state.AddToQueue(track3)

	snap := state.Snapshot()
	if snap.CurrentTrack == nil || snap.CurrentTrack.ID != "1" {
		t.Fatalf("expected track 1 to be current, got %+v", snap.CurrentTrack)
	}
	if snap.Status != StatusPlaying {
		t.Fatalf("expected StatusPlaying, got %s", snap.Status)
	}
	if len(snap.Queue) != 2 {
		t.Fatalf("expected 2 tracks in queue, got %d", len(snap.Queue))
	}

	// Simulate 25ms of playback then Pause
	time.Sleep(25 * time.Millisecond)
	state.Pause()

	pausedSnap := state.Snapshot()
	if pausedSnap.Status != StatusPaused {
		t.Fatalf("expected StatusPaused, got %s", pausedSnap.Status)
	}
	if pausedSnap.PauseOffset < 15*time.Millisecond {
		t.Fatalf("expected PauseOffset >= 15ms, got %v", pausedSnap.PauseOffset)
	}

	// Elapsed while paused should remain constant at PauseOffset
	time.Sleep(15 * time.Millisecond)
	if got := state.Elapsed(); got != pausedSnap.PauseOffset {
		t.Fatalf("expected paused elapsed %v, got %v", pausedSnap.PauseOffset, got)
	}

	// Resume Play() and verify elapsed continues from PauseOffset
	if err := state.Play(); err != nil {
		t.Fatalf("unexpected error on Play: %v", err)
	}
	if got := state.Elapsed(); got < pausedSnap.PauseOffset {
		t.Fatalf("expected resumed elapsed >= %v, got %v", pausedSnap.PauseOffset, got)
	}

	// Host telemetry update: Host pauses inside YouTube iframe at 43s / 107s
	if ok := state.UpdateFromHost("dQw4w9WgXcQ", StatusPaused, EngineYouTube, 43*time.Second, 107*time.Second); !ok {
		t.Fatalf("expected UpdateFromHost to succeed")
	}
	hostSnap := state.Snapshot()
	if hostSnap.Status != StatusPaused || hostSnap.Elapsed != 43*time.Second || hostSnap.CurrentTrack.Duration != 107*time.Second {
		t.Fatalf("unexpected state after UpdateFromHost: status=%s elapsed=%v duration=%v",
			hostSnap.Status, hostSnap.Elapsed, hostSnap.CurrentTrack.Duration)
	}

	// Reorder queue: swap track2 (idx 0) and track3 (idx 1)
	if err := state.ReorderQueue(0, 1); err != nil {
		t.Fatalf("unexpected error on ReorderQueue: %v", err)
	}
	snapAfterReorder := state.Snapshot()
	if snapAfterReorder.Queue[0].ID != "3" || snapAfterReorder.Queue[1].ID != "2" {
		t.Fatalf("unexpected queue order after reorder: %+v", snapAfterReorder.Queue)
	}

	// Skip should advance to track3 (now at index 0)
	next := state.Skip()
	if next == nil || next.ID != "3" {
		t.Fatalf("expected Skip to advance to track 3, got %+v", next)
	}

	// Remove track2 from queue
	if err := state.RemoveFromQueue("2"); err != nil {
		t.Fatalf("unexpected error on RemoveFromQueue: %v", err)
	}
	if len(state.Snapshot().Queue) != 0 {
		t.Fatalf("expected empty queue after removal")
	}

	// Final Skip clears current track and pauses
	if next := state.Skip(); next != nil {
		t.Fatalf("expected nil track on empty queue skip, got %+v", next)
	}
	finalSnap := state.Snapshot()
	if finalSnap.CurrentTrack != nil || finalSnap.Status != StatusPaused {
		t.Fatalf("expected idle paused state, got %+v", finalSnap)
	}
}

