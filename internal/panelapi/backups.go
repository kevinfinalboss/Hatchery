package panelapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

const (
	platformBackupSecret   = "hatchery-backup-platform"
	connectionSecretPrefix = "hatchery-s3-"
	// connectionLabel marks a Secret as one of the org's S3 connections (its value is the name).
	connectionLabel = "gameservers.hatchery.io/backup-connection"

	// platformConnection is the reserved connection name of the platform's own storage.
	platformConnection = "platform"
)

var connectionName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

func connectionSecretName(name string) string { return connectionSecretPrefix + name }

// BackupConfig is the platform's own backup storage, set from the panel-api flags. The Secret it
// names holds "access-key" and "secret-key". It is copied (with a per-org restic password) into an
// organization's namespace the first time that organization takes a backup there.
type BackupConfig struct {
	Endpoint        string
	Bucket          string
	SecretNamespace string
	SecretName      string
}

// Enabled reports whether the platform offers backup storage at all.
func (c BackupConfig) Enabled() bool { return c.Bucket != "" && c.SecretName != "" }

type backupLimits struct {
	MaxPerServer  int32 `json:"maxPerServer"`
	MaxPerOrg     int32 `json:"maxPerOrg"`
	RetentionDays int32 `json:"retentionDays"`
}

type connectionItem struct {
	Name     string   `json:"name"`
	Endpoint string   `json:"endpoint,omitempty"`
	Buckets  []string `json:"buckets"`
}

type backupSettingsResponse struct {
	Platform struct {
		Available bool          `json:"available"`
		Limits    *backupLimits `json:"limits,omitempty"`
	} `json:"platform"`
	Connections []connectionItem `json:"connections"`
}

func connectionFromSecret(sec *corev1.Secret) connectionItem {
	item := connectionItem{Name: sec.Labels[connectionLabel], Endpoint: string(sec.Data["endpoint"]), Buckets: []string{}}
	_ = json.Unmarshal(sec.Data["buckets"], &item.Buckets)
	return item
}

func (s *Server) listConnections(ctx context.Context, ns string) ([]corev1.Secret, error) {
	var list corev1.SecretList
	if err := s.Client.List(ctx, &list, client.InNamespace(ns), client.HasLabels{connectionLabel}); err != nil {
		return nil, err
	}
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].Labels[connectionLabel] < list.Items[j].Labels[connectionLabel]
	})
	return list.Items, nil
}

// orgBackupLimits returns the org's platform-storage limits, or nil when the platform storage is
// off for it (not configured on the platform, or the org has no backups block).
func (s *Server) orgBackupLimits(ctx context.Context, orgSlug string) (*gameserversv1alpha1.TenantBackupQuota, error) {
	if !s.Backup.Enabled() {
		return nil, nil
	}
	var tenant gameserversv1alpha1.Tenant
	if err := s.Client.Get(ctx, client.ObjectKey{Name: orgSlug}, &tenant); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return tenant.Spec.Quota.Backups, nil
}

func (s *Server) handleGetBackupSettings(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	out := backupSettingsResponse{Connections: []connectionItem{}}

	limits, err := s.orgBackupLimits(r.Context(), acc.Org.Slug)
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	if limits != nil {
		out.Platform.Available = true
		out.Platform.Limits = &backupLimits{MaxPerServer: limits.MaxPerServer, MaxPerOrg: limits.MaxPerOrg, RetentionDays: limits.RetentionDays}
	}
	secrets, err := s.listConnections(r.Context(), r.PathValue("namespace"))
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	for i := range secrets {
		out.Connections = append(out.Connections, connectionFromSecret(&secrets[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

type connectionRequest struct {
	Endpoint  string   `json:"endpoint"`
	Buckets   []string `json:"buckets"`
	AccessKey string   `json:"accessKey"`
	SecretKey string   `json:"secretKey"`
}

var (
	bucketName   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	prefixString = regexp.MustCompile(`^[A-Za-z0-9._/-]*$`)
)

const maxBucketsPerConnection = 20

func randomToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// serversUsing returns the servers whose backup destination is the given connection (and bucket,
// when bucket is not empty).
func (s *Server) serversUsing(ctx context.Context, ns, conn string, buckets map[string]bool) ([]string, error) {
	var list gameserversv1alpha1.GameServerList
	if err := s.Client.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, err
	}
	var names []string
	for i := range list.Items {
		t := list.Items[i].Spec.BackupTarget
		if t == nil || t.Connection != conn {
			continue
		}
		if buckets == nil || buckets[t.Bucket] {
			names = append(names, list.Items[i].Name)
		}
	}
	return names, nil
}

// handlePutBackupConnection creates or updates one of the org's S3 connections: endpoint, keys and
// the list of buckets the org's servers may choose from. The keys are write-only: GET never returns
// them, and leaving them out on a later save keeps the stored ones.
func (s *Server) handlePutBackupConnection(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	name := r.PathValue("connection")
	fail := func(code int, msg string) {
		s.auditEvent(r, acc.Org.Slug, "backup.connection.update", "backup-connection", name, "failed", nil)
		writeError(w, code, msg)
	}
	if !connectionName.MatchString(name) || name == platformConnection {
		fail(http.StatusBadRequest, `the connection name must be lowercase letters, digits and '-' (up to 32 characters), and "platform" is reserved`)
		return
	}

	var req connectionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		fail(http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	if len(req.Buckets) == 0 || len(req.Buckets) > maxBucketsPerConnection {
		fail(http.StatusBadRequest, fmt.Sprintf("list between 1 and %d buckets", maxBucketsPerConnection))
		return
	}
	seen := map[string]bool{}
	buckets := make([]string, 0, len(req.Buckets))
	for _, b := range req.Buckets {
		b = strings.TrimSpace(b)
		if !bucketName.MatchString(b) {
			fail(http.StatusBadRequest, fmt.Sprintf("%q is not a valid S3 bucket name", b))
			return
		}
		if !seen[b] {
			seen[b] = true
			buckets = append(buckets, b)
		}
	}
	if req.Endpoint != "" {
		u, err := url.Parse(req.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			fail(http.StatusBadRequest, "endpoint must be an http(s) URL")
			return
		}
	}

	ns := r.PathValue("namespace")
	key := client.ObjectKey{Namespace: ns, Name: connectionSecretName(name)}
	var sec corev1.Secret
	exists := true
	if err := s.Client.Get(r.Context(), key, &sec); err != nil {
		if !apierrors.IsNotFound(err) {
			fail(statusFor(err), err.Error())
			return
		}
		exists = false
	}
	if (req.AccessKey == "") != (req.SecretKey == "") || (!exists && req.AccessKey == "") {
		fail(http.StatusBadRequest, "accessKey and secretKey are both required")
		return
	}

	if exists {
		// A bucket a server sends its backups to cannot be dropped from the list.
		removed := map[string]bool{}
		for _, old := range connectionFromSecret(&sec).Buckets {
			if !seen[old] {
				removed[old] = true
			}
		}
		if len(removed) > 0 {
			users, err := s.serversUsing(r.Context(), ns, name, removed)
			if err != nil {
				fail(statusFor(err), err.Error())
				return
			}
			if len(users) > 0 {
				fail(http.StatusConflict, "a bucket you removed is the backup destination of: "+strings.Join(users, ", "))
				return
			}
		}
	} else {
		password, err := randomToken(24)
		if err != nil {
			fail(http.StatusInternalServerError, err.Error())
			return
		}
		sec = corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: ns, Labels: map[string]string{connectionLabel: name}},
			Data:       map[string][]byte{"restic-password": []byte(password)},
		}
	}
	list, _ := json.Marshal(buckets)
	sec.Data["endpoint"] = []byte(req.Endpoint)
	sec.Data["buckets"] = list
	if req.AccessKey != "" {
		sec.Data["access-key"] = []byte(req.AccessKey)
		sec.Data["secret-key"] = []byte(req.SecretKey)
	}
	var err error
	if exists {
		err = s.Client.Update(r.Context(), &sec)
	} else {
		err = s.Client.Create(r.Context(), &sec)
	}
	if err != nil {
		fail(statusFor(err), err.Error())
		return
	}
	s.auditEvent(r, acc.Org.Slug, "backup.connection.update", "backup-connection", name, "success", nil)
	s.handleGetBackupSettings(w, r)
}

// handleDeleteBackupConnection removes a connection. It refuses while a server sends its backups
// there or backups still live there: restoring and cleaning them needs these credentials.
func (s *Server) handleDeleteBackupConnection(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	ns, name := r.PathValue("namespace"), r.PathValue("connection")
	fail := func(code int, msg string) {
		s.auditEvent(r, acc.Org.Slug, "backup.connection.delete", "backup-connection", name, "failed", nil)
		writeError(w, code, msg)
	}
	users, err := s.serversUsing(r.Context(), ns, name, nil)
	if err != nil {
		fail(statusFor(err), err.Error())
		return
	}
	if len(users) > 0 {
		fail(http.StatusConflict, "this connection is the backup destination of: "+strings.Join(users, ", "))
		return
	}
	backups, err := s.listBackups(r.Context(), ns)
	if err != nil {
		fail(statusFor(err), err.Error())
		return
	}
	for i := range backups {
		if backups[i].Labels[gameserversv1alpha1.BackupDestinationLabel] == name {
			fail(http.StatusConflict, "delete the backups stored through this connection first: restoring and cleaning them needs its credentials")
			return
		}
	}
	err = s.Client.Delete(r.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: connectionSecretName(name), Namespace: ns}})
	if err != nil && !apierrors.IsNotFound(err) {
		fail(statusFor(err), err.Error())
		return
	}
	s.auditEvent(r, acc.Org.Slug, "backup.connection.delete", "backup-connection", name, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

// validateBackupTarget checks a server's requested backup destination and returns the normalized one
// (nil clears it). The int is the HTTP status for a rejection.
func (s *Server) validateBackupTarget(ctx context.Context, ns, orgSlug string, t *gameserversv1alpha1.BackupTarget) (*gameserversv1alpha1.BackupTarget, int, error) {
	if t == nil || t.Connection == "" {
		return nil, 0, nil
	}
	if t.Connection == platformConnection {
		limits, err := s.orgBackupLimits(ctx, orgSlug)
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		if limits == nil {
			return nil, http.StatusConflict, fmt.Errorf("the platform's backup storage is not available for this organization")
		}
		return &gameserversv1alpha1.BackupTarget{Connection: platformConnection}, 0, nil
	}

	var sec corev1.Secret
	if err := s.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: connectionSecretName(t.Connection)}, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, http.StatusUnprocessableEntity, fmt.Errorf("unknown backup connection %q", t.Connection)
		}
		return nil, http.StatusInternalServerError, err
	}
	inList := false
	for _, b := range connectionFromSecret(&sec).Buckets {
		inList = inList || b == t.Bucket
	}
	if !inList {
		return nil, http.StatusUnprocessableEntity, fmt.Errorf("bucket %q is not one of the buckets of connection %q", t.Bucket, t.Connection)
	}
	prefix := strings.Trim(strings.TrimSpace(t.Prefix), "/")
	if len(prefix) > 256 || !prefixString.MatchString(prefix) {
		return nil, http.StatusUnprocessableEntity, fmt.Errorf("prefix may only contain letters, digits, '.', '_', '-' and '/' (up to 256 characters)")
	}
	return &gameserversv1alpha1.BackupTarget{Connection: t.Connection, Bucket: t.Bucket, Prefix: prefix}, 0, nil
}

func (s *Server) listBackups(ctx context.Context, ns string) ([]gameserversv1alpha1.GameServerBackup, error) {
	var list gameserversv1alpha1.GameServerBackupList
	if err := s.Client.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// countsForLimits: a failed backup holds no space worth limiting, and one already being deleted is
// on its way out.
func countsForLimits(b *gameserversv1alpha1.GameServerBackup) bool {
	return b.Labels[gameserversv1alpha1.BackupDestinationLabel] == platformConnection &&
		b.Status.Phase != gameserversv1alpha1.GameServerBackupPhaseFailed && b.DeletionTimestamp.IsZero()
}

type restoreItem struct {
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	CreatedAt string `json:"createdAt"`
}

type backupItem struct {
	Name           string       `json:"name"`
	CreatedAt      string       `json:"createdAt"`
	Phase          string       `json:"phase"`
	Destination    string       `json:"destination"`
	Bucket         string       `json:"bucket,omitempty"`
	Prefix         string       `json:"prefix,omitempty"`
	ExpiresAt      string       `json:"expiresAt,omitempty"`
	CompletionTime string       `json:"completionTime,omitempty"`
	Deleting       bool         `json:"deleting,omitempty"`
	Restore        *restoreItem `json:"restore,omitempty"`
}

type backupListResponse struct {
	Items []backupItem `json:"items"`
	// Usage is what counts against the platform-storage limits.
	Usage struct {
		Server int `json:"server"`
		Org    int `json:"org"`
	} `json:"usage"`
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	ns, server := r.PathValue("namespace"), r.PathValue("name")
	all, err := s.listBackups(r.Context(), ns)
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	var restores gameserversv1alpha1.GameServerRestoreList
	if err := s.Client.List(r.Context(), &restores, client.InNamespace(ns), client.MatchingLabels{gameserversv1alpha1.BackupGameServerLabel: server}); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	latest := map[string]*gameserversv1alpha1.GameServerRestore{}
	for i := range restores.Items {
		rs := &restores.Items[i]
		if cur := latest[rs.Spec.BackupRef.Name]; cur == nil || cur.CreationTimestamp.Before(&rs.CreationTimestamp) {
			latest[rs.Spec.BackupRef.Name] = rs
		}
	}

	out := backupListResponse{Items: []backupItem{}}
	sort.Slice(all, func(i, j int) bool { return all[j].CreationTimestamp.Before(&all[i].CreationTimestamp) })
	for i := range all {
		b := &all[i]
		if countsForLimits(b) {
			out.Usage.Org++
		}
		if b.Labels[gameserversv1alpha1.BackupGameServerLabel] != server {
			continue
		}
		if countsForLimits(b) {
			out.Usage.Server++
		}
		item := backupItem{
			Name:        b.Name,
			CreatedAt:   b.CreationTimestamp.UTC().Format(time.RFC3339),
			Phase:       string(b.Status.Phase),
			Destination: b.Labels[gameserversv1alpha1.BackupDestinationLabel],
			ExpiresAt:   b.Annotations[gameserversv1alpha1.BackupExpiresAtAnnotation],
			Deleting:    !b.DeletionTimestamp.IsZero(),
		}
		if d := b.Spec.Destination.S3; d != nil {
			item.Bucket, item.Prefix = d.Bucket, d.Prefix
		}
		if b.Status.CompletionTime != nil {
			item.CompletionTime = b.Status.CompletionTime.UTC().Format(time.RFC3339)
		}
		if rs := latest[b.Name]; rs != nil {
			item.Restore = &restoreItem{Name: rs.Name, Phase: string(rs.Status.Phase), CreatedAt: rs.CreationTimestamp.UTC().Format(time.RFC3339)}
		}
		out.Items = append(out.Items, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// backupObjectName keeps the Job name (which equals the backup's) inside the 63-character limit.
func backupObjectName(prefix, server string) string {
	if len(server) > 40 {
		server = server[:40]
	}
	return fmt.Sprintf("%s%s-%s", prefix, server, strconv.FormatInt(time.Now().Unix(), 36))
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	ns, server := r.PathValue("namespace"), r.PathValue("name")

	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	target := gs.Spec.BackupTarget
	if target == nil || target.Connection == "" {
		writeError(w, http.StatusConflict, "choose where this server's backups go first")
		return
	}

	var s3 gameserversv1alpha1.S3Destination
	annotations := map[string]string{}
	if target.Connection == platformConnection {
		limits, err := s.orgBackupLimits(r.Context(), acc.Org.Slug)
		if err != nil {
			writeError(w, statusFor(err), err.Error())
			return
		}
		if limits == nil {
			writeError(w, http.StatusConflict, "the platform's backup storage is not available for this organization")
			return
		}
		all, err := s.listBackups(r.Context(), ns)
		if err != nil {
			writeError(w, statusFor(err), err.Error())
			return
		}
		var perServer, perOrg int32
		for i := range all {
			if !countsForLimits(&all[i]) {
				continue
			}
			perOrg++
			if all[i].Labels[gameserversv1alpha1.BackupGameServerLabel] == server {
				perServer++
			}
		}
		if perServer >= limits.MaxPerServer {
			writeError(w, http.StatusConflict, fmt.Sprintf("backup limit reached: this server may keep at most %d backup(s) on the platform's storage; delete one first", limits.MaxPerServer))
			return
		}
		if perOrg >= limits.MaxPerOrg {
			writeError(w, http.StatusConflict, fmt.Sprintf("backup limit reached: the organization may keep at most %d backup(s) on the platform's storage; delete one first", limits.MaxPerOrg))
			return
		}
		if err := s.ensurePlatformBackupSecret(r.Context(), ns); err != nil {
			writeError(w, http.StatusBadGateway, "could not prepare the platform's backup credentials: "+err.Error())
			return
		}
		s3 = gameserversv1alpha1.S3Destination{
			Endpoint:  s.Backup.Endpoint,
			Bucket:    s.Backup.Bucket,
			Prefix:    acc.Org.Slug + "/" + server,
			SecretRef: corev1.LocalObjectReference{Name: platformBackupSecret},
		}
		annotations[gameserversv1alpha1.BackupExpiresAtAnnotation] = time.Now().Add(time.Duration(limits.RetentionDays) * 24 * time.Hour).UTC().Format(time.RFC3339)
	} else {
		var sec corev1.Secret
		if err := s.Client.Get(r.Context(), client.ObjectKey{Namespace: ns, Name: connectionSecretName(target.Connection)}, &sec); err != nil {
			if apierrors.IsNotFound(err) {
				writeError(w, http.StatusConflict, fmt.Sprintf("the connection %q no longer exists: choose this server's backup destination again", target.Connection))
				return
			}
			writeError(w, statusFor(err), err.Error())
			return
		}
		conn := connectionFromSecret(&sec)
		inList := false
		for _, b := range conn.Buckets {
			inList = inList || b == target.Bucket
		}
		if !inList {
			writeError(w, http.StatusConflict, fmt.Sprintf("bucket %q is no longer one of connection %q: choose this server's backup destination again", target.Bucket, target.Connection))
			return
		}
		prefix := target.Prefix
		if prefix == "" {
			prefix = server
		}
		s3 = gameserversv1alpha1.S3Destination{
			Endpoint:  conn.Endpoint,
			Bucket:    target.Bucket,
			Prefix:    prefix,
			SecretRef: corev1.LocalObjectReference{Name: sec.Name},
		}
	}

	bkp := &gameserversv1alpha1.GameServerBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name: backupObjectName("", server), Namespace: ns, Annotations: annotations,
			Labels: map[string]string{
				gameserversv1alpha1.BackupGameServerLabel:  server,
				gameserversv1alpha1.BackupDestinationLabel: target.Connection,
			},
		},
		Spec: gameserversv1alpha1.GameServerBackupSpec{
			GameServerRef: gameserversv1alpha1.GameServerRef{Name: server},
			Destination:   gameserversv1alpha1.BackupDestination{S3: &s3},
		},
	}
	err := s.Client.Create(r.Context(), bkp)
	if apierrors.IsAlreadyExists(err) { // two backups in the same second
		suffix, _ := randomToken(3)
		bkp.Name += "-" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(suffix))
		bkp.ResourceVersion = ""
		err = s.Client.Create(r.Context(), bkp)
	}
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, backupItem{
		Name: bkp.Name, CreatedAt: bkp.CreationTimestamp.UTC().Format(time.RFC3339), Phase: string(bkp.Status.Phase),
		Destination: target.Connection, Bucket: s3.Bucket, Prefix: s3.Prefix,
		ExpiresAt: annotations[gameserversv1alpha1.BackupExpiresAtAnnotation],
	})
}

// ensurePlatformBackupSecret creates the org's copy of the platform's S3 credentials, with a restic
// password generated once per org. The password is never regenerated: every snapshot in the org's
// repositories is encrypted with it.
func (s *Server) ensurePlatformBackupSecret(ctx context.Context, ns string) error {
	var src corev1.Secret
	if err := s.Client.Get(ctx, client.ObjectKey{Namespace: s.Backup.SecretNamespace, Name: s.Backup.SecretName}, &src); err != nil {
		return err
	}
	access, secret := src.Data["access-key"], src.Data["secret-key"]
	if len(access) == 0 || len(secret) == 0 {
		return fmt.Errorf("secret %s/%s needs access-key and secret-key", s.Backup.SecretNamespace, s.Backup.SecretName)
	}

	var dst corev1.Secret
	err := s.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: platformBackupSecret}, &dst)
	switch {
	case apierrors.IsNotFound(err):
		password, err := randomToken(24)
		if err != nil {
			return err
		}
		return s.Client.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: platformBackupSecret, Namespace: ns},
			Data:       map[string][]byte{"access-key": access, "secret-key": secret, "restic-password": []byte(password)},
		})
	case err != nil:
		return err
	}
	if string(dst.Data["access-key"]) != string(access) || string(dst.Data["secret-key"]) != string(secret) {
		dst.Data["access-key"], dst.Data["secret-key"] = access, secret // the platform rotated its keys
		return s.Client.Update(ctx, &dst)
	}
	return nil
}

// backupOfServer finds a backup by name and makes sure it belongs to the server in the URL, so a
// backup is only ever reachable through its own server.
func (s *Server) backupOfServer(r *http.Request) (*gameserversv1alpha1.GameServerBackup, int, error) {
	var b gameserversv1alpha1.GameServerBackup
	key := client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("backup")}
	if err := s.Client.Get(r.Context(), key, &b); err != nil {
		return nil, statusFor(err), err
	}
	if b.Labels[gameserversv1alpha1.BackupGameServerLabel] != r.PathValue("name") {
		return nil, http.StatusNotFound, fmt.Errorf("backup not found for this server")
	}
	return &b, 0, nil
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	b, code, err := s.backupOfServer(r)
	if err != nil {
		writeError(w, code, err.Error())
		return
	}
	if err := s.Client.Delete(r.Context(), b); err != nil && !apierrors.IsNotFound(err) {
		writeError(w, statusFor(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	b, code, err := s.backupOfServer(r)
	if err != nil {
		writeError(w, code, err.Error())
		return
	}
	if b.Status.Phase != gameserversv1alpha1.GameServerBackupPhaseCompleted || !b.DeletionTimestamp.IsZero() {
		writeError(w, http.StatusConflict, "only a completed backup can be restored")
		return
	}
	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	if gs.Spec.State != gameserversv1alpha1.GameServerStateStopped {
		writeError(w, http.StatusConflict, "stop the server before restoring: the restore overwrites its files")
		return
	}
	rs := &gameserversv1alpha1.GameServerRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name: backupObjectName("rs-", gs.Name), Namespace: gs.Namespace,
			Labels: map[string]string{gameserversv1alpha1.BackupGameServerLabel: gs.Name},
		},
		Spec: gameserversv1alpha1.GameServerRestoreSpec{
			GameServerRef: gameserversv1alpha1.GameServerRef{Name: gs.Name},
			BackupRef:     gameserversv1alpha1.GameServerBackupRef{Name: b.Name},
		},
	}
	if err := s.Client.Create(r.Context(), rs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, restoreItem{Name: rs.Name, Phase: string(rs.Status.Phase), CreatedAt: rs.CreationTimestamp.UTC().Format(time.RFC3339)})
}
