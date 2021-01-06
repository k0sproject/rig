package winrm

import (
	"bufio"
	"bytes"
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

	ps "github.com/k0sproject/rig/powershell"
	"github.com/k0sproject/rig/util"

	"github.com/masterzen/winrm"
	log "github.com/sirupsen/logrus"
)

// Upload uploads a file to a host
// Adapted from https://github.com/jbrekelmans/go-winrm/copier.go by Jasper Brekelmans
func (c *Connection) Upload(src, dst string) error {
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
	shell, err := c.client.CreateShell()
	if err != nil {
		return err
	}
	defer shell.Close()
	log.Tracef("%s: running %s", c, psCmd)
	cmd, err := shell.Execute("powershell -ExecutionPolicy Unrestricted -EncodedCommand " + psCmd)
	if err != nil {
		return err
	}

	// Create a dummy request to get its length
	dummy := winrm.NewSendInputRequest("dummydummydummy", "dummydummydummy", "dummydummydummy", []byte(""), false, winrm.DefaultParameters)
	maxInput := len(dummy.String()) - 100
	bufferCapacity := (winrm.DefaultParameters.EnvelopeSize - maxInput) / 4 * 3
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
			b, err := cmd.Stdin.Write(base64LineBuffer)
			realSent += uint64(b)
			chunkDuration := time.Since(lastStart).Seconds()
			chunkSpeed := float64(b) / chunkDuration
			log.Tracef("%s: transfered %d bytes in %f seconds (%s/s)", c, b, chunkDuration, util.FormatBytes(uint64(chunkSpeed)))
			if ended {
				cmd.Stdin.Close()
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
		cmd.Close()
		return err
	}
	if !ended {
		log.Tracef("%s: transfering remaining chunk", c)
		_, _ = sha256DigestLocalObj.Write(buffer[:bufferLength])
		sha256DigestLocal = hex.EncodeToString(sha256DigestLocalObj.Sum(nil))
		base64.StdEncoding.Encode(base64LineBuffer, buffer[:bufferLength])
		i := base64.StdEncoding.EncodedLen(bufferLength)
		base64LineBuffer[i] = '\r'
		base64LineBuffer[i+1] = '\n'
		_, err = cmd.Stdin.Write(base64LineBuffer[:i+2])
		if err != nil {
			if !strings.Contains(err.Error(), ps.PipeHasEnded) && !strings.Contains(err.Error(), ps.PipeIsBeingClosed) {
				cmd.Close()
				return err
			}
			// ignore pipe errors that results from passing true to cmd.SendInput
		}
		cmd.Stdin.Close()
		ended = true
		bytesSent += uint64(bufferLength)
		realSent += uint64(bufferLength)
		bufferLength = 0
	}
	var wg sync.WaitGroup
	wg.Add(2)
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	go func() {
		defer wg.Done()
		_, err = io.Copy(&stderr, cmd.Stderr)
		if err != nil {
			stderr.Reset()
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(cmd.Stdout)
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
	cmd.Wait()
	wg.Wait()

	log.Tracef("%s: real sent bytes: %d (%f%% overhead)", c, realSent, 100*(1.0-(float64(bytesSent)/float64(realSent))))

	if cmd.ExitCode() != 0 {
		log.WithFields(log.Fields{
			"stdout":    stdout.String(),
			"stderr":    stderr.String(),
			"exit_code": cmd.ExitCode(),
		}).Errorf("non-zero exit code")
		return fmt.Errorf("non-zero exit code")
	}
	if sha256DigestRemote == "" {
		return fmt.Errorf("copy file command did not output the expected JSON to stdout but exited with code 0")
	} else if sha256DigestRemote != sha256DigestLocal {
		return fmt.Errorf("copy file checksum mismatch (local = %s, remote = %s)", sha256DigestLocal, sha256DigestRemote)
	}

	return nil
}
