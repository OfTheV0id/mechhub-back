package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"mechhub-back/internal/session"
	"mechhub-back/internal/solochat"
	"mechhub-back/internal/user"
)

// TestGormStackSmoke 用 sqlite 内存库跑通 user / session / solochat 三个仓库的
// 主路径,验证 Stage 2 GORM 迁移没漏改字段名 / 类型签名。
func TestGormStackSmoke(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&user.User{}, &user.Token{},
		&session.Session{},
		&solochat.Conversation{}, &solochat.UploadedFile{},
	); err != nil {
		t.Fatal("migrate:", err)
	}

	ctx := context.Background()
	uid := uuid.NewString()

	urepo := user.NewRepo(db)
	u := &user.User{
		ID: uid, Email: "a@b.c", PasswordHash: "x", Name: "Test",
		Role: user.UserRoleStudent, Verified: true, CreatedAt: time.Now(),
	}
	if err := urepo.Insert(ctx, u); err != nil {
		t.Fatal("insert user:", err)
	}
	if got, err := urepo.FindByEmail(ctx, "a@b.c"); err != nil || got.ID != uid {
		t.Fatalf("find user: %v %+v", err, got)
	}
	if got, err := urepo.FindByID(ctx, uid); err != nil || got.Email != "a@b.c" {
		t.Fatalf("find by id: %v %+v", err, got)
	}
	if err := urepo.UpdateName(ctx, uid, "Test2"); err != nil {
		t.Fatal(err)
	}

	store := session.NewStore(db, time.Hour)
	sess, err := store.New(ctx, uid)
	if err != nil {
		t.Fatal("new session:", err)
	}
	if got, err := store.Get(ctx, sess.ID); err != nil || got.UserID != uid {
		t.Fatalf("get session: %v %+v", err, got)
	}
	if err := store.DeleteByUser(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, sess.ID); err == nil {
		t.Fatal("session should be gone after DeleteByUser")
	}

	tok := &user.Token{
		ID: "tok123", UserID: uid, Kind: user.TokenKindVerify,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := urepo.InsertToken(ctx, tok); err != nil {
		t.Fatal("insert tok:", err)
	}
	if got, err := urepo.FindAndDeleteToken(ctx, "tok123", user.TokenKindVerify); err != nil || got.UserID != uid {
		t.Fatalf("consume token: %v %+v", err, got)
	}
	if _, err := urepo.FindAndDeleteToken(ctx, "tok123", user.TokenKindVerify); err == nil {
		t.Fatal("token should be gone after consume")
	}

	srepo := solochat.NewRepo(db)
	conv := &solochat.Conversation{
		ID: uuid.NewString(), UserID: uid, Title: "test",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := srepo.InsertConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	if convs, err := srepo.ListConversations(ctx, uid); err != nil || len(convs) != 1 {
		t.Fatalf("list conversations: %v len=%d", err, len(convs))
	}
	if err := srepo.UpdateConversationTitle(ctx, conv.ID, uid, "renamed"); err != nil {
		t.Fatal(err)
	}
	if got, err := srepo.FindConversation(ctx, conv.ID, uid); err != nil || got.Title != "renamed" {
		t.Fatalf("find conversation: %v %+v", err, got)
	}

	f := &solochat.UploadedFile{
		ID: uuid.NewString(), OwnerUserID: uid, OSSKey: "k", OriginalName: "n",
		MimeType: "image/jpeg", Kind: solochat.FileKindImage, Size: 100, CreatedAt: time.Now(),
	}
	if err := srepo.InsertFile(ctx, f); err != nil {
		t.Fatal(err)
	}
	if files, err := srepo.FindFilesByIDs(ctx, []string{f.ID}, uid); err != nil || len(files) != 1 {
		t.Fatalf("find files: %v len=%d", err, len(files))
	}
}
