// Package offsite takes MatrixCtrl's full archive off the server (etappe 120).
//
// A backup that lives on the machine it protects dies with it, and one that depends on
// somebody remembering to click a download button is not taken. This package builds the
// same archive the browser download builds, encrypts all of it for a public key the
// operator holds the private half of, and streams it to an S3-compatible bucket.
//
// The shape is deliberately a pipeline of streams, never a file:
//
//	CreateFull → age → io.Pipe → PutObject(size unknown)
//
// What the provider stores is unreadable to the provider and, because only the public
// key lives on the server, unreadable to whoever takes the server over as well.
package offsite
