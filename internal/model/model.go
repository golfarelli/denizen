// Package model holds the plain data structures shared across the
// repository, service and handler layers.
package model

// User is one Denizen account. Created only by redeeming an invite — see
// Invite.
type User struct {
	ID               string
	Username         string
	PasswordHash     string
	IsAdmin          bool
	QuotaBytes       int64
	StorageUsedBytes int64
	Disabled         bool
	CreatedAt        int64
}

// Invite is a one-time code that lets someone create a User. The very first
// invite on a fresh install (the "bootstrap invite", CreatedBy == "") grants
// admin; every invite an admin creates afterwards does not, unless set
// explicitly.
type Invite struct {
	ID          string
	Code        string
	CreatedBy   string // empty for the bootstrap invite
	GrantsAdmin bool
	QuotaBytes  *int64 // nil = use the server's default quota
	ExpiresAt   int64
	UsedAt      *int64
	UsedBy      *string
	CreatedAt   int64
}

// RefreshToken is one issued (and revocable) refresh token. Only its hash is
// ever persisted — see internal/token.
type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt int64
	RevokedAt *int64
	CreatedAt int64
}

// Item is a file or a folder. Folders group other items under them via
// ParentID; a nil ParentID means "the owner's root". See
// docs/ARCHITECTURE.md for how an item's ID maps to a real path on disk.
type Item struct {
	ID        string
	OwnerID   string
	ParentID  *string
	Name      string
	Type      ItemType
	SizeBytes int64
	MimeType  *string
	Checksum  *string
	DeletedAt *int64 // nil = active; set = in trash since this time
	CreatedAt int64
	UpdatedAt int64
}

type ItemType string

const (
	ItemTypeFile   ItemType = "file"
	ItemTypeFolder ItemType = "folder"
)

// Share is a public link granting access to one Item. See
// internal/db/migrations/0001_initial_schema.sql for why only TokenHash —
// never the raw token — is persisted.
type Share struct {
	ID           string
	ItemID       string
	TokenHash    string
	CreatedBy    string
	RequiresAuth bool
	ExpiresAt    *int64 // nil = never expires
	CreatedAt    int64
}

// SharePermission is how much a UserShare grant lets its recipient do:
// view (read/download/preview only) or edit (also modify content, and for
// a folder grant, create/upload/rename/move/delete within it — see
// ItemService.CanEdit and .resolveGrant).
type SharePermission string

const (
	SharePermissionView SharePermission = "view"
	SharePermissionEdit SharePermission = "edit"
)

// UserShare is a direct grant of one Item to one specific User — "share
// with a person" (like Google Drive), distinct from Share above (a link
// anyone holding it can use). Works for folders too, not just files: a
// grant on a folder is inherited by everything nested inside it (see
// ItemService.resolveGrant), at the same Permission level. See
// internal/db/migrations/0002_user_shares.sql and 0003_user_share_permission.sql.
type UserShare struct {
	ID           string
	ItemID       string
	OwnerID      string // must match the item's own OwnerID at creation time
	SharedWithID string
	Permission   SharePermission
	CreatedAt    int64
}
