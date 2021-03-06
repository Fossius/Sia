package renter

import (
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/NebulousLabs/Sia/crypto"
	"github.com/NebulousLabs/Sia/encoding"
	"github.com/NebulousLabs/Sia/modules"
)

var (
	downloadAttempts = 5
)

// A Download is a file download that has been queued by the renter. It
// implements the modules.DownloadInfo interface.
type Download struct {
	// Implementation note: received is declared first to ensure that it is
	// 64-bit aligned. This is necessary to ensure that atomic operations work
	// correctly on ARM and x86-32.
	received uint64

	startTime   time.Time
	complete    bool
	filesize    uint64
	destination string
	nickname    string

	pieces []filePiece
	file   *os.File
}

// StartTime returns when the download was initiated.
func (d *Download) StartTime() time.Time {
	return d.startTime
}

// Complete returns whether the file is ready to be used.
func (d *Download) Complete() bool {
	return d.complete
}

// Filesize returns the size of the file.
func (d *Download) Filesize() uint64 {
	return d.filesize
}

// Received returns the number of bytes downloaded so far.
func (d *Download) Received() uint64 {
	return d.received
}

// Destination returns the file's location on disk.
func (d *Download) Destination() string {
	return d.destination
}

// Nickname returns the identifier assigned to the file when it was uploaded.
func (d *Download) Nickname() string {
	return d.nickname
}

// Write implements the io.Writer interface. Each write updates the Download's
// received field. This allows download progress to be monitored in real-time.
func (d *Download) Write(b []byte) (int, error) {
	n, err := d.file.Write(b)
	// atomically update d.received
	// TODO: atomic operations may not be necessary
	atomic.AddUint64(&d.received, uint64(n))
	return n, err
}

// downloadPiece attempts to retrieve a file piece from a host.
func (d *Download) downloadPiece(piece filePiece) error {
	conn, err := net.DialTimeout("tcp", string(piece.HostIP), 10e9)
	if err != nil {
		return err
	}
	defer conn.Close()
	err = encoding.WriteObject(conn, [8]byte{'R', 'e', 't', 'r', 'i', 'e', 'v', 'e'})
	if err != nil {
		return err
	}

	// Send the ID of the contract for the file piece we're requesting.
	if err := encoding.WriteObject(conn, piece.ContractID); err != nil {
		return err
	}

	// Simultaneously download, decrypt, and calculate the Merkle root of the file.
	tee := io.TeeReader(
		// Use a LimitedReader to ensure we don't read indefinitely.
		io.LimitReader(conn, int64(piece.Contract.FileSize)),
		// Write the decrypted bytes to the file.
		piece.EncryptionKey.NewWriter(d),
	)
	merkleRoot, err := crypto.ReaderMerkleRoot(tee)
	if err != nil {
		return err
	}

	if merkleRoot != piece.Contract.FileMerkleRoot {
		return errors.New("host provided a file that's invalid")
	}

	return nil
}

// newDownload initializes a new Download object.
func newDownload(file *file, destination string) (*Download, error) {
	// Create the download destination file.
	handle, err := os.Create(destination)
	if err != nil {
		return nil, err
	}

	// Filter out the inactive pieces.
	var activePieces []filePiece
	for _, piece := range file.Pieces {
		if piece.Active {
			activePieces = append(activePieces, piece)
		}
	}
	if len(activePieces) == 0 {
		return nil, errors.New("no active pieces")
	}

	return &Download{
		startTime: time.Now(),
		complete:  false,
		// for now, all the pieces are equivalent
		filesize:    file.Pieces[0].Contract.FileSize,
		received:    0,
		destination: destination,
		nickname:    file.Name,

		pieces: activePieces,
		file:   handle,
	}, nil
}

// Download downloads a file, identified by its nickname, to the destination
// specified.
func (r *Renter) Download(nickname, destination string) error {
	lockID := r.mu.Lock()
	// Lookup the file associated with the nickname.
	file, exists := r.files[nickname]
	if !exists {
		return errors.New("no file of that nickname")
	}

	// Create the download object and spawn the download process.
	d, err := newDownload(file, destination)
	if err != nil {
		return err
	}

	// Add the download to the download queue.
	r.downloadQueue = append(r.downloadQueue, d)
	r.mu.Unlock(lockID)

	// Download the file. We only need one piece, so iterate through the hosts
	// until a download succeeds.
	for i := 0; i < downloadAttempts; i++ {
		for _, piece := range d.pieces {
			downloadErr := d.downloadPiece(piece)
			if downloadErr == nil {
				// done
				d.complete = true
				d.file.Close()
				return nil
			}
			// Reset seek, since the file may have been partially written. The
			// next attempt will overwrite these bytes.
			d.file.Seek(0, 0)
			atomic.SwapUint64(&d.received, 0)
		}

		// This iteration failed, no hosts returned the piece. Try again
		// after waiting a random amount of time.
		randSource := make([]byte, 1)
		rand.Read(randSource)
		time.Sleep(time.Second * time.Duration(i*i) * time.Duration(randSource[0]))
	}

	// File could not be downloaded; delete the copy on disk.
	d.file.Close()
	os.Remove(destination)

	return errors.New("could not download any file pieces")
}

// DownloadQueue returns the list of downloads in the queue.
func (r *Renter) DownloadQueue() []modules.DownloadInfo {
	lockID := r.mu.RLock()
	defer r.mu.RUnlock(lockID)

	// order from most recent to least recent
	downloads := make([]modules.DownloadInfo, len(r.downloadQueue))
	for i := range r.downloadQueue {
		downloads[i] = r.downloadQueue[len(r.downloadQueue)-i-1]
	}
	return downloads
}
