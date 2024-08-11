package bedrock

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Backupper interface {
	Backup(files []File) error
}

type BackupperFunc func(files []File) error

// BackupperFunc implements the Backupper interface.
func (f BackupperFunc) Backup(files []File) error {
	return f(files)
}

func DummyBackup(files []File) error {
	for _, f := range files {
		fmt.Printf("DummyBackup file: %s - %d\n", f.Name, f.Length)
	}
	return nil
}

type TarBackup struct {
	Writer io.Writer
}

func (t TarBackup) Backup(files []File) error {
	tw := tar.NewWriter(t.Writer)
	for _, file := range files {
		if err := addFileToTar(tw, file); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return nil
}

func addFileToTar(tw *tar.Writer, file File) error {
	p := realFilePath(file)

	info, err := os.Stat(filepath.Join("worlds", p))
	if err != nil {
		return err
	}
	in, err := os.Open(filepath.Join("worlds", p))
	if err != nil {
		return err
	}
	defer in.Close()

	hdr := &tar.Header{
		Name:       p,
		Mode:       int64(info.Mode()),
		Size:       file.Length,
		ChangeTime: info.ModTime(),
		ModTime:    info.ModTime(),
		AccessTime: info.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	n, err := io.CopyN(tw, in, file.Length)
	if err != nil {
		return err
	}
	if n != file.Length {
		return fmt.Errorf("bytes copied for %s did match %d != %d",
			file.Name, n, file.Length)
	}
	return nil
}

// copyBackup writes files into a temp directory.
func copyBackup(files []File) error {
	tmpdir, err := os.MkdirTemp("", "backup")
	if err != nil {
		return err
	}

	for _, f := range files {
		err := copyFile(f, tmpdir)
		if err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies the given file to the dest directory. The target directory
// is created if it does not exist.
func copyFile(file File, dest string) error {
	p := realFilePath(file)
	in, err := os.Open(filepath.Join("worlds", p))
	if err != nil {
		return err
	}
	defer in.Close()

	destPath := filepath.Join(dest, p)
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	n, err := io.CopyN(out, in, file.Length)
	if err != nil {
		return err
	}
	if n != file.Length {
		return fmt.Errorf("bytes copied for %s did match %d != %d",
			file.Name, n, file.Length)
	}
	return nil
}

// realFilePath returns the real relative location of the path.
// Older older versions of the bedrock server (< 1.16) returned the wrong path
// for most files in the results, since they were located under a seperate `db`
// directory.
func realFilePath(file File) string {
	base := filepath.Base(file.Name)
	dir := filepath.Dir(file.Name)
	// If the given file does not exist assume it's missing the 'db'
	// subdirectory.
	if _, err := os.Stat(filepath.Join("worlds", file.Name)); err != nil {
		dir = filepath.Join(dir, "db")
	}
	return filepath.Join(dir, base)
}

// BackupOptions
type BackupOptions struct {
	// Backupper that will
	Backupper Backupper
	// CommandTimeout time to wait for the expected response.
	CommandTimeout time.Duration
	// SavePause is the delay after the save command.
	SavePause time.Duration
}

func defaultOptions(opts BackupOptions) (BackupOptions, error) {
	b := opts
	if b.Backupper == nil {
		return b, fmt.Errorf("Backupper is mandatory but is nil")
	}
	if b.CommandTimeout == 0 {
		b.CommandTimeout = time.Duration(3) * time.Second
	}
	if b.SavePause == 0 {
		b.SavePause = time.Duration(3) * time.Second
	}
	return b, nil
}

// Backup holds, queries, call external backups runction and resumes the
// server. See bedrock_server_how_to.html of the bedrock server download for
// more information.
// Warning, when this returns an error it could leave the server in an unstable
// state.
func (s *Server) Backup(ctx context.Context, opts BackupOptions) error {
	opts, err := defaultOptions(opts)
	if err != nil {
		return err
	}

	// Hold
	s.backupMutex.Lock()
	defer s.backupMutex.Unlock()
	// Always try to resume the server in case of an error. When the server
	// continues to be in a hold state future backups will fail and it will
	// also hang in case the stop command is given.
	defer func() {
		if err != nil {
			s.saveResume(ctx, time.Duration(5)*time.Minute)
		}
	}()

	err = s.saveHold(ctx, opts.CommandTimeout)
	if err != nil {
		return err
	}
	select {
	case <-time.After(opts.SavePause):
	case <-ctx.Done():
		return ctx.Err()
	}

	// Query
	var output string
	for retry := 3; retry > 0; retry-- {
		output, err = s.saveQuery(ctx, opts.CommandTimeout)
		if err == nil {
			break
		}
		time.Sleep(opts.SavePause)
	}
	if err != nil {
		return err
	}

	// Backup
	files, err := parseSaveQuery(output)
	if err != nil {
		return err
	}
	err = opts.Backupper.Backup(files)
	if err != nil {
		return err
	}

	// Resume
	err = s.saveResume(ctx, opts.CommandTimeout)
	if err != nil {
		return err
	}
	return nil
}

type File struct {
	Name   string
	Length int64
}

// parseSaveQuery returns the files with their length from the `save query`
// command response.
func parseSaveQuery(response string) ([]File, error) {
	lines := strings.Split(response, "\n")
	if len(lines) < 1 {
		return []File{}, fmt.Errorf("not enough lines in response")
	}
	var resultLine string
	for _, line := range lines {
		if strings.Contains(line, "levelname.txt") {
			resultLine = line
			break
		}
	}
	if resultLine == "" {
		return []File{}, fmt.Errorf("no results fount in given response")
	}
	entries := strings.Split(lines[len(lines)-1], ", ")
	files := make([]File, len(entries))
	for i, e := range entries {
		f := strings.Split(e, ":")
		if len(f) < 2 {
			return files, fmt.Errorf("invalid entry: %s", e)
		}
		length, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			return files, fmt.Errorf("invalid entry: %s", e)
		}
		files[i] = File{
			Name:   f[0],
			Length: length,
		}
	}
	return files, nil
}
