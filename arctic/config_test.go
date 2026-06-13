package arctic

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigPathsUnderDataDir(t *testing.T) {
	t.Setenv(EnvDataDir, "/tmp/arctic-test")
	c := DefaultConfig()
	if c.DataDir != "/tmp/arctic-test" {
		t.Fatalf("DataDir = %q", c.DataDir)
	}
	if want := filepath.Join("/tmp/arctic-test", "raw"); c.RawDir != want {
		t.Errorf("RawDir = %q, want %q", c.RawDir, want)
	}
	if want := filepath.Join("/tmp/arctic-test", "work"); c.WorkDir != want {
		t.Errorf("WorkDir = %q, want %q", c.WorkDir, want)
	}
	if c.ChunkLines != DefaultChunkLines {
		t.Errorf("ChunkLines = %d, want %d", c.ChunkLines, DefaultChunkLines)
	}
}

func TestDefaultConfigEnvOverrides(t *testing.T) {
	t.Setenv(EnvDataDir, "/tmp/arctic-test")
	t.Setenv(EnvRawDir, "/mnt/raw")
	t.Setenv(EnvWorkDir, "/mnt/work")
	t.Setenv(EnvChunkLines, "12345")
	t.Setenv(EnvMinFreeGB, "7")

	c := DefaultConfig()
	if c.RawDir != "/mnt/raw" {
		t.Errorf("RawDir = %q, want the env value", c.RawDir)
	}
	if c.WorkDir != "/mnt/work" {
		t.Errorf("WorkDir = %q, want the env value", c.WorkDir)
	}
	if c.ChunkLines != 12345 {
		t.Errorf("ChunkLines = %d, want 12345", c.ChunkLines)
	}
	if c.MinFreeGB != 7 {
		t.Errorf("MinFreeGB = %d, want 7", c.MinFreeGB)
	}
}

func TestEnvIntRejectsNonNumeric(t *testing.T) {
	t.Setenv(EnvChunkLines, "100k")
	if c := DefaultConfig(); c.ChunkLines != DefaultChunkLines {
		t.Errorf("a non-numeric ARCTIC_CHUNK_LINES should leave the default, got %d", c.ChunkLines)
	}
}
