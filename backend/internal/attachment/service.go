package attachment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
)

// Service keeps the rows and hands the bytes to the store.
type Service struct {
	db    *db.Cluster
	store Store
	log   *slog.Logger
}

// NewService makes the service and has it told when an issue is deleted, so
// the files on it are not left behind in the bucket.
func NewService(cluster *db.Cluster, store Store, issues *issue.Service) *Service {
	s := &Service{db: cluster, store: store, log: slog.Default()}
	issues.Observe(s)
	return s
}

// IssueCreated has nothing to do: an issue starts with no files.
func (s *Service) IssueCreated(context.Context, db.DBTX, *issue.Issue, issue.Actor) error { return nil }

// IssueTransitioned has nothing to do: files do not care about status.
func (s *Service) IssueTransitioned(context.Context, db.DBTX, *issue.Issue, *issue.Issue, issue.Actor) error {
	return nil
}

// CommentAdded has nothing to do.
func (s *Service) CommentAdded(context.Context, db.DBTX, uuid.UUID, bool, issue.Actor) error {
	return nil
}

// IssueDeleted leaves a tombstone for every object the issue's files occupy.
// The rows cascade with the issue; the reaper removes the bytes afterwards.
func (s *Service) IssueDeleted(ctx context.Context, tx db.DBTX, deleted *issue.Issue, _ issue.Actor) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO attachment_tombstone (object_key, org_id)
		SELECT object_key, org_id FROM attachment WHERE issue_id = $1
		ON CONFLICT (object_key) DO NOTHING`, deleted.ID)
	if err != nil {
		return fmt.Errorf("mark attachments of %s for removal: %w", deleted.Key, err)
	}
	return nil
}

const selectAttachment = `
SELECT a.id, a.issue_id, p.key || '-' || i.key_num, a.file_name, a.content_type, a.size_bytes, a.object_key, a.created_at,
       u.id, u.name, u.email, u.avatar_url
FROM attachment a
JOIN issue i ON i.id = a.issue_id
JOIN project p ON p.id = i.project_id
LEFT JOIN app_user u ON u.id = a.uploader_id`

// stored is a row with the one column a client is never shown.
type stored struct {
	Attachment
	objectKey string
}

func scan(row pgx.Row) (*stored, error) {
	var (
		a                  stored
		uid                *uuid.UUID
		name, mail, avatar *string
	)
	err := row.Scan(&a.ID, &a.IssueID, &a.IssueKey, &a.FileName, &a.ContentType, &a.Size, &a.objectKey, &a.CreatedAt,
		&uid, &name, &mail, &avatar)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if uid != nil {
		a.Uploader = &issue.UserRef{ID: *uid, Name: deref(name), Email: deref(mail), AvatarURL: deref(avatar)}
	}
	return &a, nil
}

// List is the files on an issue, oldest first.
func (s *Service) List(ctx context.Context, issueKey string) ([]Attachment, error) {
	projectKey, num, err := issue.ParseKey(issueKey)
	if err != nil {
		return nil, err
	}
	out := []Attachment{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM issue i JOIN project p ON p.id = i.project_id WHERE p.key = $1 AND i.key_num = $2)`,
			projectKey, num).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return issue.ErrNotFound
		}
		rows, err := tx.Query(ctx, selectAttachment+` WHERE p.key = $1 AND i.key_num = $2 ORDER BY a.created_at, a.id`, projectKey, num)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, a.Attachment)
		}
		return rows.Err()
	})
	return out, err
}

// UploadInput is one file as it arrived.
type UploadInput struct {
	FileName    string
	ContentType string
	Body        io.Reader
}

// Upload stores a file on an issue.
//
// The bytes go to the store inside the transaction that writes the row, so a
// store that refuses leaves no row behind. The other way round, a commit that
// fails after the bytes are stored, leaves an object nobody can reach; that is
// cheap and harmless, where a row without bytes is a broken link.
func (s *Service) Upload(ctx context.Context, issueKey string, in UploadInput, actor issue.Actor) (*Attachment, db.LSN, error) {
	projectKey, num, err := issue.ParseKey(issueKey)
	if err != nil {
		return nil, 0, err
	}
	name := CleanName(in.FileName)

	// The whole file is read first so its size is known and the limit is
	// enforced before anything is written anywhere.
	data, err := io.ReadAll(io.LimitReader(in.Body, MaxSize+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(data)) > MaxSize {
		return nil, 0, fmt.Errorf("%w: the limit is %d MB", ErrTooLarge, MaxSize>>20)
	}
	if len(data) == 0 {
		return nil, 0, ErrEmpty
	}
	contentType := contentTypeFor(in.ContentType, data)

	var created *Attachment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			issueID uuid.UUID
			orgID   uuid.UUID
		)
		err := tx.QueryRow(ctx, `
			SELECT i.id, i.org_id FROM issue i JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&issueID, &orgID)
		if errors.Is(err, pgx.ErrNoRows) {
			return issue.ErrNotFound
		}
		if err != nil {
			return err
		}

		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		key := objectKey(orgID, issueID, id, name)
		if _, err := tx.Exec(ctx, `
			INSERT INTO attachment (id, org_id, issue_id, uploader_id, file_name, content_type, size_bytes, object_key)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, $6, $7)`,
			id, issueID, actor.UserID, name, contentType, len(data), key); err != nil {
			return fmt.Errorf("record attachment: %w", err)
		}
		if err := s.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
			return err
		}
		if err := issue.RecordChanges(ctx, tx, issueID, actor.UserID, []issue.Change{{Field: "attachment", To: name}}); err != nil {
			return err
		}
		found, err := scan(tx.QueryRow(ctx, selectAttachment+` WHERE a.id = $1`, id))
		if err != nil {
			return err
		}
		created = &found.Attachment
		return events.EmitInTenant(ctx, tx, events.TopicAttachmentAdded, map[string]any{
			"issueId":      issueID,
			"key":          created.IssueKey,
			"attachmentId": id,
			"fileName":     name,
			"actorId":      actor.UserID,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return created, lsn, nil
}

// Open reads an attachment and its bytes. The caller closes the reader.
func (s *Service) Open(ctx context.Context, id uuid.UUID) (*Attachment, io.ReadCloser, error) {
	var found *stored
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scan(tx.QueryRow(ctx, selectAttachment+` WHERE a.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	body, err := s.store.Get(ctx, found.objectKey)
	if err != nil {
		return nil, nil, err
	}
	return &found.Attachment, body, nil
}

// Get reads what is known about an attachment without touching the store.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Attachment, error) {
	var found *stored
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scan(tx.QueryRow(ctx, selectAttachment+` WHERE a.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &found.Attachment, nil
}

// Delete removes an attachment. The row goes inside the transaction and a
// tombstone is written beside it; the object goes after the commit, because an
// object without a row is unreachable and harmless while a row without an
// object is a broken link. If the object cannot be removed now, the tombstone
// stays and the reaper gets it later.
func (s *Service) Delete(ctx context.Context, id uuid.UUID, actor issue.Actor) (db.LSN, error) {
	var found *stored
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scan(tx.QueryRow(ctx, selectAttachment+` WHERE a.id = $1 FOR UPDATE OF a`, id))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO attachment_tombstone (object_key, org_id) VALUES ($1, current_org_id())
			ON CONFLICT (object_key) DO NOTHING`, found.objectKey); err != nil {
			return err
		}
		return issue.RecordChanges(ctx, tx, found.IssueID, actor.UserID, []issue.Change{{Field: "attachment", From: found.FileName}})
	})
	if err != nil {
		return 0, err
	}
	if err := s.reap(ctx, found.objectKey, s.db.Write); err != nil {
		s.log.Warn("attachment object left for the reaper", "key", found.objectKey, "error", err)
	}
	return lsn, nil
}

// reap removes one object and then its tombstone, with whichever transaction
// the caller has: a tenant's own after a delete, the admin's from the reaper.
func (s *Service) reap(ctx context.Context, key string, write func(context.Context, func(context.Context, db.DBTX) error) (db.LSN, error)) error {
	if err := s.store.Delete(ctx, key); err != nil {
		return err
	}
	_, err := write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM attachment_tombstone WHERE object_key = $1`, key)
		return err
	})
	return err
}

// objectKey lays objects out by tenant, issue and attachment, so a bucket can
// be read by a person and a tenant's files can be found without the database.
func objectKey(orgID, issueID, id uuid.UUID, name string) string {
	return path.Join("org", orgID.String(), "issue", issueID.String(), id.String(), name)
}

// MaxNameLength keeps a file name short enough for a header and a listing.
const MaxNameLength = 200

// CleanName reduces a file name to something safe to store and to send back
// in a header: no directories, no control characters, never empty.
func CleanName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || r == '"' || r == '/' {
			continue
		}
		b.WriteRune(r)
	}
	name = b.String()
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if runes := []rune(name); len(runes) > MaxNameLength {
		name = string(runes[:MaxNameLength])
	}
	return name
}

// contentTypeFor trusts a specific type the client sent and sniffs the bytes
// otherwise, so a browser that says nothing still gets a sensible download.
func contentTypeFor(declared string, data []byte) string {
	declared = strings.TrimSpace(declared)
	if declared != "" && declared != "application/octet-stream" && !strings.ContainsAny(declared, "\r\n") {
		return declared
	}
	return http.DetectContentType(data)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
