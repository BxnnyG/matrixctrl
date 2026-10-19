package offsite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Target is where backups go. The access keys are not part of it: they live in a
// Kubernetes Secret (see creds.go) and are never serialised with the rest.
type Target struct {
	// Endpoint is a host, host:port or a full URL. https is assumed when no scheme is
	// given; http:// is accepted for a store inside the cluster.
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
	Bucket   string `json:"bucket"`
	Prefix   string `json:"prefix"`
	// PathStyle addresses the bucket as host/bucket instead of bucket.host. MinIO and
	// several self-hosted stores need it; the big providers want the other.
	PathStyle bool `json:"path_style"`
}

// Credentials are the S3 access key pair.
type Credentials struct {
	AccessKey string
	SecretKey string
}

// ParseEndpoint splits an endpoint into what the client wants: host[:port] and whether
// TLS is used.
func ParseEndpoint(e string) (host string, secure bool, err error) {
	e = strings.TrimSpace(e)
	if e == "" {
		return "", false, errors.New("Endpunkt: bitte die Adresse des S3-Anbieters angeben (z. B. s3.example.org)")
	}
	secure = true
	if !strings.Contains(e, "://") {
		e = "https://" + e
	}
	u, err := url.Parse(e)
	if err != nil || u.Host == "" {
		return "", false, errors.New("Endpunkt: das ist keine gültige Adresse")
	}
	switch u.Scheme {
	case "https":
	case "http":
		secure = false
	default:
		return "", false, errors.New("Endpunkt: nur https:// oder http:// ist möglich")
	}
	if p := strings.Trim(u.Path, "/"); p != "" || u.RawQuery != "" || u.User != nil {
		return "", false, errors.New("Endpunkt: ohne Pfad, Benutzer oder Parameter angeben; den Bucket trägt man im eigenen Feld ein")
	}
	return u.Host, secure, nil
}

// partSize bounds the object: S3 allows 10 000 parts, and the length of the stream is
// unknown when the upload starts, so the part size has to be chosen up front. 32 MiB
// reaches about 320 GB — far past a homeserver's archive — and costs one such buffer in
// memory while uploading.
const partSize = 32 << 20

type s3Store struct {
	c      *minio.Client
	bucket string
}

// NewS3Store connects to the bucket described by t. It makes no request; the first one
// happens with the first call.
func NewS3Store(t Target, c Credentials) (Store, error) {
	host, secure, err := ParseEndpoint(t.Endpoint)
	if err != nil {
		return nil, err
	}
	if t.Bucket == "" {
		return nil, errors.New("Bucket: bitte den Namen des Buckets angeben")
	}
	lookup := minio.BucketLookupAuto
	if t.PathStyle {
		lookup = minio.BucketLookupPath
	}
	cl, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure:       secure,
		Region:       t.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("S3-Verbindung: %w", err)
	}
	return &s3Store{c: cl, bucket: t.Bucket}, nil
}

func (s *s3Store) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	info, err := s.c.PutObject(ctx, s.bucket, key, r, -1, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
		PartSize:    partSize,
	})
	if err != nil {
		// The client aborts its own multipart upload on most failures. "Most" is not
		// good enough here: a leftover upload is invisible in a listing, is billed, and
		// the plan was explicit that no half object may stay behind. Cleaned up on a
		// fresh context, because the reason for the failure may be that ctx ended.
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_ = s.c.RemoveIncompleteUpload(cctx, s.bucket, key)
		return 0, explain(err)
	}
	return info.Size, nil
}

func (s *s3Store) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	obj, err := s.c.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, explain(err)
	}
	// GetObject is lazy; Stat is what turns "no such key" into an error now rather than
	// in the middle of a download.
	st, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, 0, explain(err)
	}
	return obj, st.Size, nil
}

func (s *s3Store) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	for o := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if o.Err != nil {
			return nil, explain(o.Err)
		}
		out = append(out, Object{Key: o.Key, Size: o.Size, LastModified: o.LastModified})
	}
	return out, nil
}

func (s *s3Store) Remove(ctx context.Context, key string) error {
	return explain(s.c.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}))
}

// explain turns S3's error codes into what to do about them. The provider's own message
// follows, since it is the thing to search for when the hint is not enough.
func explain(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	var hint string
	switch resp.Code {
	case "AccessDenied":
		hint = "Zugriff verweigert: Der Zugang hat für diesen Bucket nicht die nötigen Rechte (Lesen, Schreiben, Löschen, Auflisten)."
	case "InvalidAccessKeyId":
		hint = "Die Zugangskennung (Access Key) ist dem Anbieter unbekannt."
	case "SignatureDoesNotMatch":
		hint = "Der geheime Schlüssel (Secret Key) passt nicht zur Zugangskennung, oder die Region stimmt nicht."
	case "NoSuchBucket":
		hint = "Diesen Bucket gibt es nicht (oder nicht unter diesem Zugang)."
	case "NoSuchKey":
		hint = "Das Objekt gibt es nicht (mehr)."
	case "AuthorizationHeaderMalformed", "InvalidRegion":
		hint = "Die Region stimmt nicht mit dem Bucket überein."
	}
	if hint != "" {
		return fmt.Errorf("%s (S3: %s)", hint, resp.Code)
	}
	return err
}
