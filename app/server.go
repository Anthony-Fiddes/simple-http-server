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
	"slices"
	"strconv"
	"strings"
)

type ResponseHead struct {
	Protocol string
	Status   int
	Reason   string
	// Headers stores keys in lower case, since RFC9110 says they're case
	// insensitive. In a more serious project, this could warrant its own type
	// with Get() and Set() methods.
	Headers map[string]string
}

// Bytes returns all the bytes of an HTTP response except the body.
func (r ResponseHead) Bytes() []byte {
	if r.Protocol == "" {
		r.Protocol = "HTTP/1.1"
	}

	var result bytes.Buffer
	result.WriteString(fmt.Sprintf("%s %d", r.Protocol, r.Status))
	result.WriteString(" ")
	if r.Reason != "" {
		result.WriteString(r.Reason)
	}
	result.WriteString("\r\n")

	for header, val := range r.Headers {
		result.WriteString(header)
		result.WriteString(": ")
		result.WriteString(val)
		result.WriteString("\r\n")
	}
	result.WriteString("\r\n")

	return result.Bytes()
}

var (
	okResponse       = ResponseHead{Status: 200, Reason: "OK"}
	createdResponse  = ResponseHead{Status: 201, Reason: "Created"}
	notFoundResponse = ResponseHead{Status: 404, Reason: "Not Found"}
	errorResponse    = ResponseHead{Status: 500, Reason: "Internal Server Error"}
)

type RequestLine struct {
	// Method is all uppercase
	Method string
	// Path should always start with a /
	Path     string
	Protocol string
}

func parseRequestLine(line string) (RequestLine, error) {
	result := RequestLine{}
	// A valid start line would look like "GET /index.html HTTP/1.1"
	line = strings.TrimRight(line, "\r\n")
	sl := strings.Split(line, " ")
	if len(sl) != 3 {
		return result, fmt.Errorf("invalid start line: '%s'", line)
	}
	result.Method = sl[0]
	result.Path = sl[1]
	result.Protocol = sl[2]

	return result, nil
}

type Request struct {
	RequestLine
	Headers map[string]string
	Body    io.Reader
}

// body is not guaranteed to throw an EOF
type HandlerFunc func(req Request) (response io.Reader, err error)

type endpointHandler struct {
	prefix  string
	handler HandlerFunc
}

// TODO: Add logger (e.g. should tell you which handler failed), middleware may
// be the route to go for the next stage (compression)
//
// TODO: Add a deadline so that net conns don't just block forever if there's
// some kind of error.

// Server is a basic HTTP server that can be configured by registering handlers
// for different endpoints (i.e. request paths that begin with a given prefix).
type Server struct {
	Address          string
	listener         net.Listener
	endPointHandlers []endpointHandler
}

// RegisterHandler makes it so that the specified handler runs on any request
// path that starts with endpointPrefix.
//
// Note that "/" is a special case. It will only match if the requested path is
// "/" exactly.
func (s *Server) RegisterHandler(endpointPrefix string, handler HandlerFunc) {
	if s.endPointHandlers == nil {
		s.endPointHandlers = make([]endpointHandler, 0)
	} else {
		for i := range s.endPointHandlers {
			if s.endPointHandlers[i].prefix == endpointPrefix {
				s.endPointHandlers[i].handler = handler
				return
			}
		}
	}

	e := endpointHandler{endpointPrefix, handler}
	s.endPointHandlers = append(s.endPointHandlers, e)
	// always sort the most specific endpoint handlers earlier in the array
	slices.SortFunc(s.endPointHandlers, func(a endpointHandler, b endpointHandler) int {
		return len(b.prefix) - len(a.prefix)
	})
}

// Start only returns an error if the server could not start listening for
// requests.
func (s *Server) Start() error {
	l, err := net.Listen("tcp", s.Address)
	if err != nil {
		return err
	}
	s.listener = l
	defer s.listener.Close()

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

func getHandler(ep []endpointHandler, path string) HandlerFunc {
	for i := range ep {
		prefix := ep[i].prefix
		if prefix == "/" {
			if path == "/" {
				return ep[i].handler
			}
			continue
		}
		if strings.HasPrefix(path, prefix) {
			return ep[i].handler
		}
	}
	return nil
}

// if handleRequest fails, it wasn't able to send a response back on the conn
func (s *Server) handleRequest(conn io.ReadWriter) error {
	buf := bufio.NewReader(conn)
	requestLineStr, err := buf.ReadString('\n')
	// we should be able to scan at least one line
	if err != nil {
		return fmt.Errorf("read from connection: %w", err)
	}
	requestLine, err := parseRequestLine(requestLineStr)
	if err != nil {
		return err
	}

	headers := make(map[string]string)
	for {
		line, err := buf.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read headers: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		// there are no more headers to read
		if line == "" {
			break
		}

		h := strings.Split(line, ": ")
		key := strings.ToLower(h[0])
		value := h[1]
		headers[key] = value
	}

	handler := getHandler(s.endPointHandlers, requestLine.Path)
	if handler != nil {
		response, err := handler(Request{requestLine, headers, buf})
		if err != nil {
			return err
		}
		_, err = io.Copy(conn, response)
		if err != nil {
			return fmt.Errorf("write response: %w", err)
		}
		return nil
	}

	// if no handler is found, return a 404
	_, err = conn.Write(notFoundResponse.Bytes())
	if err != nil {
		return fmt.Errorf("write 404 response: %w", err)
	}
	return nil
}

func (s *Server) Close() error {
	return fmt.Errorf("close server: %w", s.listener.Close())
}

type closesAtEOF struct {
	R io.ReadCloser
}

func (c *closesAtEOF) Read(buf []byte) (int, error) {
	n, err := c.R.Read(buf)
	if err == io.EOF {
		c.R.Close()
	}
	return n, err
}

func getFilesEndpoint(directory string) HandlerFunc {
	filesEndpoint := func(directory string, req Request) (io.Reader, error) {
		fileName, err := parsePathArg(req.Path)
		filePath := path.Join(directory, fileName)
		// Normally we would respond that we don't support any methods besides GET
		// and POST. For now we'll just make the GET request the default
		// functionality.
		if req.Method != "POST" {
			file, err := os.Open(filePath)
			if errors.Is(err, fs.ErrNotExist) {
				return bytes.NewBuffer(notFoundResponse.Bytes()), nil
			}
			if err != nil {
				return nil, err
			}
			stats, err := os.Stat(filePath)
			if err != nil {
				return nil, err
			}

			headers := make(map[string]string, 2)
			headers["content-type"] = "application/octet-stream"
			headers["content-length"] = fmt.Sprintf("%d", stats.Size())
			head := okResponse
			head.Headers = headers
			headBuf := bytes.NewBuffer(head.Bytes())
			return io.MultiReader(headBuf, &closesAtEOF{file}), nil
		}

		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		contentLength, ok := req.Headers["content-length"]
		if !ok {
			return nil, errors.New("no 'Content-Length' header in request")
		}
		length, err := strconv.Atoi(contentLength)
		if err != nil {
			return nil, err
		}

		_, err = io.CopyN(file, req.Body, int64(length))
		if err != nil {
			return nil, fmt.Errorf("write '%s': %w", filePath, err)
		}

		return bytes.NewBuffer(createdResponse.Bytes()), nil
	}

	return func(req Request) (io.Reader, error) {
		return filesEndpoint(directory, req)
	}
}

// NOTE: Proper handlers would probably return a 405 for unsupported methods on
// an endpoint.

func rootEndpoint(req Request) (io.Reader, error) {
	r := bytes.NewBuffer(okResponse.Bytes())
	return r, nil
}

func userAgentEndpoint(req Request) (io.Reader, error) {
	// it's okay if it's not in headers, we'll just get ""
	userAgent := req.Headers["user-agent"]
	headers := make(map[string]string, 2)
	headers["Content-Type"] = "text/plain"
	headers["Content-Length"] = fmt.Sprintf("%d", len(userAgent))
	head := okResponse
	head.Headers = headers
	headBuf := bytes.NewBuffer(head.Bytes())
	body := bytes.NewBufferString(userAgent)
	return io.MultiReader(headBuf, body), nil
}

func parsePathArg(requestPath string) (string, error) {
	path := strings.TrimLeft(requestPath, "/")
	pp := strings.Split(path, "/")
	if len(pp) < 2 {
		// TODO: would be nice to return an error response to the client letting them
		// know that they didn't include the necessary argument for the
		// endpoint. Something like:
		// "/echo requires an argument. E.g. /echo/hey_there"
		return "", fmt.Errorf("'%s' does not contain a path argument", requestPath)
	}
	endpoint := pp[0]
	arg := path[len(endpoint)+1:]
	return arg, nil
}

func echoEndpoint(req Request) (io.Reader, error) {
	arg, err := parsePathArg(req.Path)
	if err != nil {
		return nil, err
	}
	headers := make(map[string]string, 2)
	headers["Content-Type"] = "text/plain"
	headers["Content-Length"] = fmt.Sprintf("%d", len(arg))
	head := okResponse
	head.Headers = headers
	headBuf := bytes.NewBuffer(head.Bytes())
	body := bytes.NewBufferString(arg)
	return io.MultiReader(headBuf, body), nil
}

func main() {
	directory := flag.String("directory", ".", "Directory to serve.")
	flag.Parse()

	s := Server{
		Address: "0.0.0.0:4221",
	}
	s.RegisterHandler("/", rootEndpoint)
	s.RegisterHandler("/user-agent", userAgentEndpoint)
	// added / at the end since this endpoint takes a path argument
	s.RegisterHandler("/echo/", echoEndpoint)
	s.RegisterHandler("/files/", getFilesEndpoint(*directory))
	err := s.Start()
	if err != nil {
		log.Printf("Could not start server: %s", err)
	}
}
