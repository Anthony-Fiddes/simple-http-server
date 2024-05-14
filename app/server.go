package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
)

type HTTPResponseHead struct {
	version string
	status  int
	reason  string
	headers map[string]string
}

func (h HTTPResponseHead) Bytes() []byte {
	if h.version == "" {
		h.version = "HTTP/1.1"
	}

	var result bytes.Buffer
	result.WriteString(fmt.Sprintf("%s %d", h.version, h.status))
	result.WriteString(" ")
	if h.reason != "" {
		result.WriteString(h.reason)
	}
	result.WriteString("\r\n")

	for header, val := range h.headers {
		result.WriteString(header)
		result.WriteString(": ")
		result.WriteString(val)
		result.WriteString("\r\n")
	}
	result.WriteString("\r\n")

	return result.Bytes()
}

var (
	okResponse       = HTTPResponseHead{status: 200, reason: "OK"}
	createdResponse  = HTTPResponseHead{status: 201, reason: "Created"}
	notFoundResponse = HTTPResponseHead{status: 404, reason: "Not Found"}
	errorResponse    = HTTPResponseHead{status: 500, reason: "Internal Server Error"}
)

const filesEndpoint = "/files/"

type FileServer struct {
	Address   string
	Directory string
	listener  net.Listener
}

// Start only returns an error if the server could not start listening for
// requests.
func (s *FileServer) Start() error {
	l, err := net.Listen("tcp", s.Address)
	if err != nil {
		return err
	}
	s.listener = l
	defer s.listener.Close()

	if s.Directory == "" {
		s.Directory = "./"
	}

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// don't get blocked on logging
			go func() {
				log.Print("FileServer failed to accept connection: ", err.Error())
			}()
			continue
		}

		go func() {
			defer conn.Close()
			err := s.handleRequest(conn)
			if err != nil {
				log.Printf("error handling FileServer request: %s", err)
				// TODO: is this where we should send the 500 response?
				_, err := conn.Write(errorResponse.Bytes())
				if err != nil {
					log.Printf("FileServer failed to send 500 response: %s", err)
				}
			}
		}()
	}
}

func (s *FileServer) Close() error {
	return fmt.Errorf("close server: %w", s.listener.Close())
}

func (s *FileServer) handleRequest(conn net.Conn) error {
	buf := bufio.NewReader(conn)
	startLine, err := buf.ReadString('\n')
	// we should be able to scan at least one line
	if err != nil {
		return fmt.Errorf("read from connection: %w", err)
	}
	startLine = strings.TrimRight(startLine, "\r\n")
	// startLine would look like "GET /index.html HTTP/1.1"
	sl := strings.Split(startLine, " ")
	method := sl[0]
	requestPath := sl[1]

	headers := make(map[string]string)
	for {
		line, err := buf.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read headers from connection: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		// there are no more headers to read
		if line == "" {
			break
		}

		h := strings.Split(line, ": ")
		key := h[0]
		value := h[1]
		headers[key] = value
	}

	if method == "POST" && strings.HasPrefix(requestPath, filesEndpoint) {
		fileName := requestPath[len(filesEndpoint):]
		filePath := path.Join(s.Directory, fileName)
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer file.Close()

		contentLength, ok := headers["Content-Length"]
		if !ok {
			return fmt.Errorf("no 'Content-Length' header in request")
		}
		length, err := strconv.Atoi(contentLength)
		if err != nil {
			return err
		}

		_, err = io.CopyN(file, buf, int64(length))
		if err != nil {
			return err
		}

		_, err = conn.Write(createdResponse.Bytes())
		if err != nil {
			return err
		}

	} else if strings.HasPrefix(requestPath, "/echo/") {
		arg := requestPath[len("/echo/"):]
		headers := make(map[string]string, 2)
		headers["Content-Type"] = "text/plain"
		headers["Content-Length"] = fmt.Sprintf("%d", len(arg))
		responseHead := okResponse
		responseHead.headers = headers
		_, err := conn.Write(responseHead.Bytes())
		if err != nil {
			return err
		}
		_, err = conn.Write([]byte(arg))
		if err != nil {
			return err
		}
	} else if strings.HasPrefix(requestPath, filesEndpoint) {
		fileName := requestPath[len(filesEndpoint):]
		filePath := path.Join(s.Directory, fileName)
		file, err := os.Open(filePath)
		if errors.Is(err, fs.ErrNotExist) {
			conn.Write(notFoundResponse.Bytes())
			return nil
		}
		if err != nil {
			return err
		}
		defer file.Close()
		stats, err := os.Stat(filePath)
		if err != nil {
			return err
		}

		headers := make(map[string]string, 2)
		headers["Content-Type"] = "application/octet-stream"
		headers["Content-Length"] = fmt.Sprintf("%d", stats.Size())
		responseHead := okResponse
		responseHead.headers = headers
		_, err = conn.Write(responseHead.Bytes())
		if err != nil {
			return err
		}
		_, err = io.Copy(conn, file)
		if err != nil {
			return err
		}
	} else if requestPath == "/user-agent" {
		// it's okay if it's not in headers, we'll just get ""
		userAgent := headers["User-Agent"]
		headers := make(map[string]string, 2)
		headers["Content-Type"] = "text/plain"
		headers["Content-Length"] = fmt.Sprintf("%d", len(userAgent))
		responseHead := okResponse
		responseHead.headers = headers
		_, err := conn.Write(responseHead.Bytes())
		if err != nil {
			return err
		}
		_, err = conn.Write([]byte(userAgent))
		if err != nil {
			return err
		}
	} else if requestPath == "/" {
		_, err := conn.Write(okResponse.Bytes())
		if err != nil {
			return err
		}
	} else {
		_, err := conn.Write(notFoundResponse.Bytes())
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	directory := flag.String("directory", ".", "Directory to serve.")
	flag.Parse()

	s := FileServer{
		Address:   "0.0.0.0:4221",
		Directory: *directory,
	}
	err := s.Start()
	if err != nil {
		log.Printf("Could not start server: %s", err)
	}
}
