package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"golang.org/x/crypto/bcrypt"
)

func runSeeder() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	driver := strings.ToLower(strings.TrimSpace(cfg.Database.Driver))
	if driver == "" {
		driver = "postgres"
	}

	if driver == "sqlite" {
		return runSQLiteSeeder(cfg)
	}

	db, err := database.NewPostgresConnection(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to db: %w", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Seed test user
	email := "test@example.com"
	username := "testuser"
	password := "password123"

	// Check if user exists
	var exists bool
	err = db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)", email).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check existing user: %w", err)
	}

	if !exists {
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		_, err = db.Pool.Exec(ctx, `
            INSERT INTO users (email, username, password_hash, created_at, updated_at)
            VALUES ($1, $2, $3, $4, $4)
        `, email, username, string(hashedPassword), time.Now())
		if err != nil {
			return fmt.Errorf("failed to insert user: %w", err)
		}
		fmt.Println("✅ Seeded test user: test@example.com / password123")
	} else {
		fmt.Println("ℹ️  Test user already exists")
	}

	return nil
}

func runSQLiteSeeder(cfg *config.Config) error {
	sqliteDB, err := database.NewSQLiteConnectionWithExtension(cfg.Database.SQLitePath, cfg.Database.SQLiteVectorExtensionPath)
	if err != nil {
		return fmt.Errorf("failed to connect to sqlite db: %w", err)
	}
	defer func() {
		_ = sqliteDB.Close()
	}()

	ctx := context.Background()
	telegramID := "seed-test-user"

	var exists bool
	err = sqliteDB.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE telegram_id = ?)", telegramID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check existing sqlite user: %w", err)
	}

	if !exists {
		now := time.Now()
		_, err = sqliteDB.DB.ExecContext(ctx, `
			INSERT INTO users (telegram_id, risk_level, created_at)
			VALUES (?, 'medium', ?)
		`, telegramID, now)
		if err != nil {
			return fmt.Errorf("failed to insert sqlite user: %w", err)
		}
		fmt.Println("✅ Seeded test user: telegram_id = seed-test-user")
	} else {
		fmt.Println("ℹ️  Test user already exists")
	}

	return nil
}
