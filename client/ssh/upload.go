package ssh

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/alessio/shellescape"
	ps "github.com/k0sproject/rig/powershell"
)

func (c *Client) uploadLinux(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	hostIn, err := session.StdinPipe()
	if err != nil {
		return err
	}

	gw, err := gzip.NewWriterLevel(hostIn, gzip.BestSpeed)
	if err != nil {
		return err
	}

	err = session.Start(fmt.Sprintf(`gzip -d > %s`, shellescape.Quote(dst)))
	if err != nil {
		return err
	}

	io.Copy(gw, in)
	gw.Close()
	hostIn.Close()

	return session.Wait()
}

func (c *Client) uploadWindows(src, dst string) error {
	psCmd := ps.UploadCmd(dst)
	stat, err := os.Stat(src)
	if err != nil {
		return err
	}
	sha256DigestLocalObj := sha256.New()
	sha256DigestLocal := ""
	sha256DigestRemote := ""
	srcSize := uint64(stat.Size())
	bytesSent := uint64(0)
	realSent := uint64(0)
	fdClosed := false
	fd, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if !fdClosed {
			_ = fd.Close()
			fdClosed = true
		}
	}()
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	hostIn, err := session.StdinPipe()
	if err != nil {
		return err
	}
	hostOut, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	hostErr, err := session.StderrPipe()
	if err != nil {
		return err
	}

	psRunCmd := "powershell -ExecutionPolicy Unrestricted -EncodedCommand " + psCmd
	if err := session.Start(psRunCmd); err != nil {
		return err
	}

	bufferCapacity := 262143 // use 256kb chunks
	base64LineBufferCapacity := bufferCapacity/3*4 + 2
	base64LineBuffer := make([]byte, base64LineBufferCapacity)
	base64LineBuffer[base64LineBufferCapacity-2] = '\r'
	base64LineBuffer[base64LineBufferCapacity-1] = '\n'
	buffer := make([]byte, bufferCapacity)
	bufferLength := 0
	ended := false

	for {
		lastStart := time.Now()
		var n int
		n, err = fd.Read(buffer)
		bufferLength += n
		if err != nil {
			break
		}
		if bufferLength == bufferCapacity {
			base64.StdEncoding.Encode(base64LineBuffer, buffer)
			bytesSent += uint64(bufferLength)
			_, _ = sha256DigestLocalObj.Write(buffer)
			if bytesSent >= srcSize {
				ended = true
				sha256DigestLocal = hex.EncodeToString(sha256DigestLocalObj.Sum(nil))
			}
			b, err := hostIn.Write(base64LineBuffer)
			realSent += uint64(b)
			chunkDuration := time.Since(lastStart).Seconds()
			chunkSpeed := float64(b) / chunkDuration
			if ended {
				hostIn.Close()
			}

			bufferLength = 0
			if err != nil {
				return err
			}
		}
	}
	_ = fd.Close()
	fdClosed = true
	if err == io.EOF {
		err = nil
	}
	if err != nil {
		return err
	}
	if !ended {
		_, _ = sha256DigestLocalObj.Write(buffer[:bufferLength])
		sha256DigestLocal = hex.EncodeToString(sha256DigestLocalObj.Sum(nil))
		base64.StdEncoding.Encode(base64LineBuffer, buffer[:bufferLength])
		i := base64.StdEncoding.EncodedLen(bufferLength)
		base64LineBuffer[i] = '\r'
		base64LineBuffer[i+1] = '\n'
		_, err = hostIn.Write(base64LineBuffer[:i+2])
		if err != nil {
			if !strings.Contains(err.Error(), ps.PipeHasEnded) && !strings.Contains(err.Error(), ps.PipeIsBeingClosed) {
				return err
			}
			// ignore pipe errors that results from passing true to cmd.SendInput
		}
		hostIn.Close()
		ended = true
		bytesSent += uint64(bufferLength)
		realSent += uint64(bufferLength)
		bufferLength = 0
	}
	var wg sync.WaitGroup
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(&stderr, hostErr)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(hostOut)
		for scanner.Scan() {
			var output struct {
				Sha256 string `json:"sha256"`
			}
			if json.Unmarshal(scanner.Bytes(), &output) == nil {
				sha256DigestRemote = output.Sha256
			} else {
				_, _ = stdout.Write(scanner.Bytes())
				_, _ = stdout.WriteString("\n")
			}
		}
		if err := scanner.Err(); err != nil {
			stdout.Reset()
		}
	}()
	session.Wait()
	wg.Wait()

	if sha256DigestRemote == "" {
		return fmt.Errorf("copy file command did not output the expected JSON to stdout but exited with code 0")
	} else if sha256DigestRemote != sha256DigestLocal {
		return fmt.Errorf("copy file checksum mismatch (local = %s, remote = %s)", sha256DigestLocal, sha256DigestRemote)
	}

	return nil
}
