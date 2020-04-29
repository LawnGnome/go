package gc

import (
	stderrors "errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	syscall "golang.org/x/sys/unix"
)

type jobserverClient struct {
	fdRead  int
	fdWrite int
}

type jobserverToken struct {
	client *jobserverClient
	token  byte
}

func newJobserverClient() (*jobserverClient, error) {
	makeFlags := os.Getenv("MAKEFLAGS")
	if makeFlags == "" {
		// Not running under make.
		return nil, nil
	}

	jsc := &jobserverClient{}

	for _, arg := range strings.Split(makeFlags, " ") {
		if strings.HasPrefix(arg, "--jobserver-auth=") || strings.HasPrefix(arg, "--jobserver-fds=") {
			var err error

			value := strings.SplitN(arg, "=", 2)[1]
			fds := strings.SplitN(value, ",", 2)
			jsc.fdRead, err = strconv.Atoi(fds[0])
			if err != nil {
				return nil, fmt.Errorf("cannot parse jobserver read fd: %v", err)
			}

			jsc.fdWrite, err = strconv.Atoi(fds[1])
			if err != nil {
				return nil, fmt.Errorf("cannot parse jobserver write fd: %v", err)
			}

			goto gotFDs
		}
	}

	return nil, fmt.Errorf("cannot parse --jobserver-auth from $MAKEFLAGS: %s", makeFlags)

gotFDs:
	if checkFD(jsc.fdRead) != nil || checkFD(jsc.fdWrite) != nil {
		return nil, stderrors.New("unable to read from jobserver file descriptors; does make know this can work with a jobserver (try adding + before the go invocation)")
	}

	if err := setBlocking(jsc.fdRead); err != nil {
		return nil, stderrors.New("cannot set read descriptor as blocking")
	}

	if err := setBlocking(jsc.fdWrite); err != nil {
		return nil, stderrors.New("cannot set write descriptor as blocking")
	}

	return jsc, nil
}

func (jsc *jobserverClient) Acquire() (*jobserverToken, error) {
	token := []byte{0}
	log.Printf("attempting to acquire jobserver token")
	rv, err := syscall.Read(jsc.fdRead, token)
	if err != nil {
		return nil, fmt.Errorf("cannot read from jobserver FD: %w", err)
	}
	if rv != 1 {
		return nil, fmt.Errorf("got unexpected number of bytes from jobserver FD: %d", rv)
	}
	log.Printf("acquired jobserver token")
	return &jobserverToken{token: token[0], client: jsc}, nil
}

func (jst *jobserverToken) Release() error {
	if jst == nil {
		return nil
	}

	log.Printf("releasing jobserver token")
	rv, err := syscall.Write(jst.client.fdWrite, []byte{jst.token})
	log.Printf("released jobserver token")
	if err != nil {
		return fmt.Errorf("cannot write to jobserver FD: %w", err)
	}
	if rv != 1 {
		return fmt.Errorf("wrote unexpected number of bytes to jobserver FD: %d", rv)
	}
	return nil
}

func checkFD(fd int) error {
	return syscall.Fstat(fd, &syscall.Stat_t{})
}

func setBlocking(fd int) error {
	flags, err := syscall.FcntlInt(uintptr(fd), syscall.F_GETFL, 0)
	if err != nil {
		return err
	}

	if flags&syscall.O_NONBLOCK == syscall.O_NONBLOCK {
		_, err := syscall.FcntlInt(uintptr(fd), syscall.F_SETFL, flags&^syscall.O_NONBLOCK)
		return err
	}

	return nil
}
