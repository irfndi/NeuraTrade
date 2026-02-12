package main

import (
	"context"
	"database/sql"
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
	email := "test@example.com"
	username := "testuser"
	telegramID := "seed-test-user"
	password := "password123"

	userColumns, err := sqliteUserColumns(ctx, sqliteDB.DB)
	if err != nil {
		return fmt.Errorf("failed to inspect sqlite users schema: %w", err)
	}

	var exists bool
	if userColumns["email"] {
		err = sqliteDB.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE email = ?)", email).Scan(&exists)
	} else {
		err = sqliteDB.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE telegram_id = ?)", telegramID).Scan(&exists)
	}
	if err != nil {
		return fmt.Errorf("failed to check existing sqlite user: %w", err)
	}

	if !exists {
		hashedPassword, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return fmt.Errorf("failed to hash password: %w", hashErr)
		}

		now := time.Now()
		if userColumns["email"] {
			_, err = sqliteDB.DB.ExecContext(ctx, `
            INSERT INTO users (email, username, password_hash, role, created_at, updated_at)
            VALUES (?, ?, ?, 'user', ?, ?)
        `, email, username, string(hashedPassword), now, now)
		} else {
			_, err = sqliteDB.DB.ExecContext(ctx, `
            INSERT INTO users (telegram_id, risk_level, created_at)
            VALUES (?, 'medium', ?)
        `, telegramID, now)
		}
		if err != nil {
			return fmt.Errorf("failed to insert sqlite user: %w", err)
		}
		fmt.Println("✅ Seeded test user: test@example.com / password123")
	} else {
		fmt.Println("ℹ️  Test user already exists")
	}

	return nil
}

func sqliteUserColumns(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite database handle is nil")
	}

	rows, err := db.QueryContext(ctx, "PRAGMA table_info(users)")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[strings.ToLower(strings.TrimSpace(name))] = true
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("users table has no columns")
	}

	return columns, nil
}
