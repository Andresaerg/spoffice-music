# SKILL: Repository Pattern and Centralized SQL Queries in Go

## METADATA

- **Name:** go_repository_queries_skill
- **Description:** Automatically triggers when creating, modifying, or refactoring data access layers, handlers, services, or any database interactions in Go.
- **Triggers:** Keywords such as "go repository", "database struct", "sql query go", "interface repository", "crud go", "data layer".

## ARCHITECTURE INSTRUCTIONS

Strictly apply the following idiomatic Go design rules to ensure clean separation of concerns and testability:

### 1. Repository Interfaces and Structures

- **Interfaces First:** Always define a public repository interface (e.g., `UserRepository`). This decouples the business logic and allows easy unit testing using mocks.
- **Hidden Implementation:** The concrete struct implementing the interface must be private/unexported (e.g., `sqlUserRepository`) and exposed via a public constructor function (e.g., `NewSQLUserRepository(db *sql.DB) UserRepository`).
- **Context Injection:** Every repository method interacting with the database MUST accept `context.Context` as its first parameter (e.g., `FindByID(ctx context.Context, id int64)`).

### 2. Centralized Queries File (`queries.go`)

- **Zero Inline Hardcoding:** Writing SQL query strings directly inside the repository implementation methods is strictly forbidden.
- **Dedicated File:** Create a separate file named `queries.go` (or `entity_queries.go`) within the same data/repository package.
- **Unexported Constants:** Keep query strings clean and encapsulated by defining them as unexported constants within a `const` block. Use descriptive naming (e.g., `findUserByIDQuery`).
- **Parameterized Security:** Use safe bind vars/placeholders exclusively (e.g., `$1, $2` for PostgreSQL or `?` for MySQL). Never use string concatenation or `fmt.Sprintf` to inject variables into query strings.

## REFERENCE EXAMPLE (Idiomatic Go Code)

### Queries File (`internal/user/queries.go`)

```go
package user

const (
	findUserByIDQuery = `
		SELECT id, name, email, created_at
		FROM users
		WHERE id = $1 AND deleted_at IS NULL;
	`
	createUserQuery = `
		INSERT INTO users (name, email, password)
		VALUES ($1, $2, $3)
		RETURNING id;
	`
)
```

### Repository File (`internal/user/repository.go`)

```go
package user

import (
	"context"
	"database/sql"
)

// User represents the domain entity
type User struct {
	ID    int64
	Name  string
	Email string
}

// UserRepository defines the contract for data operations
type UserRepository interface {
	FindByID(ctx context.Context, id int64) (*User, error)
}

// sqlUserRepository implements the interface using a SQL database connection
type sqlUserRepository struct {
	db *sql.DB
}

// NewSQLUserRepository is the factory constructor for the repository
func NewSQLUserRepository(db *sql.DB) UserRepository {
	return &sqlUserRepository{db: db}
}

// FindByID fetches a single user by consuming the centralized query constant
func (r *sqlUserRepository) FindByID(ctx context.Context, id int64) (*User, error) {
	var u User

	// The repository safely references the 'findUserByIDQuery' from queries.go
	err := r.db.QueryRowContext(ctx, findUserByIDQuery, id).Scan(&u.ID, &u.Name, &u.Email)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Or return a custom domain error like ErrNotFound
		}
		return nil, err
	}

	return &u, nil
}
```

## QUALITY RESTRAINTS

- If the user requests a database operation inside an HTTP Handler or Service, **refuse to write inline SQL there**. Force the architecture to route the request through the defined Repository Interface.
- Always handle database-specific errors gracefully (like `sql.ErrNoRows`) to prevent leaking internal database implementation details to outer layers.
