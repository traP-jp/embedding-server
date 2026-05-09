package main

import (
	"fmt"
	"path/filepath"
	"testing"

	sqliteDriver "github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"embedding-server/api/model"
)

func TestGORMGlebarezSQLiteReadWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rw.db")
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)",
		filepath.ToSlash(dbPath),
	)
	db, err := gorm.Open(sqliteDriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.EmbeddingJob{}, &model.EmbeddingCache{}); err != nil {
		t.Fatal(err)
	}

	job := model.EmbeddingJob{
		Payload: []byte(`{}`),
		Status:  "pending",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	var got model.EmbeddingJob
	if err := db.First(&got, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	got.Status = "done"
	if err := db.Save(&got).Error; err != nil {
		t.Fatal(err)
	}
	var after model.EmbeddingJob
	if err := db.First(&after, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != "done" {
		t.Fatalf("status: got %q", after.Status)
	}

	key := "cache-key"
	cache := model.EmbeddingCache{Key: key, Value: []byte(`{"n":1}`)}
	if err := db.Create(&cache).Error; err != nil {
		t.Fatal(err)
	}
	var c2 model.EmbeddingCache
	if err := db.Where("key = ?", key).First(&c2).Error; err != nil {
		t.Fatal(err)
	}
	if string(c2.Value) != `{"n":1}` {
		t.Fatalf("cache value: got %s", c2.Value)
	}
}
