// Package pvefake is an in-memory fake of the Proxmox VE REST API
// (/api2/json) for tests. It speaks just enough of the protocol for the
// Telmate SDK: ticket login, API token auth, cluster resources, qemu
// config CRUD, lifecycle endpoints, and the async task status endpoint.
package pvefake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Guest is one qemu VM or lxc container held by the fake cluster.
type Guest struct {
	VMID   int
	Name   string
	Node   string
	Type   string // qemu | lxc
	Status string // running | stopped
	Config map[string]any
}

// Server is a fake Proxmox VE API server.
type Server struct {
	*httptest.Server

	mu     sync.Mutex
	guests map[int]*Guest
	nextID int
	// failNext maps an operation (create, clone, update, delete, start,
	// stop) to a task exit status; the next matching task fails with it.
	failNext map[string]string
	// Token, when set, is the only accepted PVEAPIToken value.
	Token string

	taskSeq     int
	taskResults map[string]string // upid -> exitstatus
}

// New starts a fake server. Call Close when done.
func New() *Server {
	s := &Server{
		guests:      map[int]*Guest{},
		nextID:      100,
		failNext:    map[string]string{},
		taskResults: map[string]string{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// URL returns the base URL including the /api2/json prefix.
func (s *Server) URL() string { return s.Server.URL + "/api2/json" }

// AddGuest seeds a guest into the fake cluster.
func (s *Server) AddGuest(g Guest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g.Config == nil {
		g.Config = map[string]any{}
	}
	if g.Type == "" {
		g.Type = "qemu"
	}
	if g.Status == "" {
		g.Status = "stopped"
	}
	copied := g
	s.guests[g.VMID] = &copied
	if g.VMID >= s.nextID {
		s.nextID = g.VMID + 1
	}
}

// Guest returns a copy of the guest with the given vmid, or nil.
func (s *Server) Guest(vmid int) *Guest {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.guests[vmid]
	if !ok {
		return nil
	}
	copied := *g
	copied.Config = map[string]any{}
	for k, v := range g.Config {
		copied.Config[k] = v
	}
	return &copied
}

// FailNext makes the next task of the given operation fail with the
// given exit status (e.g. "unable to create VM - disk full").
func (s *Server) FailNext(op, exitStatus string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext[op] = exitStatus
}

// fakeTicket is the auth ticket issued by the fake /access/ticket endpoint.
const fakeTicket = "PVE:fake-ticket"

var (
	rxConfig = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/(qemu|lxc)/(\d+)/config$`)
	rxClone  = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/(qemu|lxc)/(\d+)/clone$`)
	rxGuest  = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/(qemu|lxc)/(\d+)$`)
	rxStatus = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/(qemu|lxc)/(\d+)/status/(start|stop)$`)
	rxCreate = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/(qemu|lxc)$`)
	rxTask   = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/tasks/([^/]+)/status$`)
)

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api2/json/access/ticket" && r.Method == http.MethodPost {
		s.handleTicket(w, r)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "authentication failure", http.StatusUnauthorized)
		return
	}

	path := r.URL.Path
	switch {
	case path == "/api2/json/cluster/resources":
		s.handleResources(w, r)
	case path == "/api2/json/cluster/nextid":
		s.mu.Lock()
		id := s.nextID
		s.mu.Unlock()
		writeData(w, strconv.Itoa(id))
	case rxTask.MatchString(path):
		s.handleTaskStatus(w, r)
	case rxConfig.MatchString(path):
		s.handleConfig(w, r)
	case rxClone.MatchString(path) && r.Method == http.MethodPost:
		s.handleClone(w, r)
	case rxStatus.MatchString(path) && r.Method == http.MethodPost:
		s.handleLifecycle(w, r)
	case rxCreate.MatchString(path) && r.Method == http.MethodPost:
		s.handleCreate(w, r)
	case rxGuest.MatchString(path) && r.Method == http.MethodDelete:
		s.handleDelete(w, r)
	default:
		http.Error(w, fmt.Sprintf("no handler for %s %s", r.Method, path), http.StatusNotImplemented)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "PVEAPIToken=") {
		return s.Token == "" || auth == "PVEAPIToken="+s.Token
	}
	// Ticket auth: the SDK sends "Authorization: PVEAuthCookie=<ticket>".
	if strings.HasPrefix(auth, "PVEAuthCookie=") {
		return strings.TrimPrefix(auth, "PVEAuthCookie=") == fakeTicket
	}
	if _, err := r.Cookie("PVEAuthCookie"); err == nil {
		return true
	}
	return false
}

func (s *Server) handleTicket(w http.ResponseWriter, r *http.Request) {
	// The SDK sends a url-encoded body without a Content-Type header,
	// so parse the body directly instead of relying on ParseForm.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "authentication failure", http.StatusUnauthorized)
		return
	}
	values, err := url.ParseQuery(string(body))
	if err != nil || values.Get("username") == "" || values.Get("password") == "" {
		http.Error(w, "authentication failure", http.StatusUnauthorized)
		return
	}
	writeData(w, map[string]any{
		"ticket":              fakeTicket,
		"CSRFPreventionToken": "fake-csrf",
		"username":            values.Get("username"),
	})
}

func (s *Server) handleResources(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]any, 0, len(s.guests))
	for _, g := range s.guests {
		list = append(list, map[string]any{
			"id":     fmt.Sprintf("%s/%d", g.Type, g.VMID),
			"vmid":   float64(g.VMID),
			"name":   g.Name,
			"node":   g.Node,
			"type":   g.Type,
			"status": g.Status,
		})
	}
	writeData(w, list)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	m := rxConfig.FindStringSubmatch(r.URL.Path)
	vmid, _ := strconv.Atoi(m[3])

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		g, ok := s.guests[vmid]
		var config map[string]any
		if ok {
			config = map[string]any{}
			for k, v := range g.Config {
				config[k] = v
			}
		}
		s.mu.Unlock()
		if !ok {
			http.Error(w, fmt.Sprintf("Configuration file 'nodes/%s/qemu-server/%d.conf' does not exist", m[1], vmid), http.StatusInternalServerError)
			return
		}
		writeData(w, config)
	case http.MethodPut:
		params := formParams(r)
		s.mu.Lock()
		g, ok := s.guests[vmid]
		if ok {
			for k, v := range params {
				g.Config[k] = v
			}
			if name, has := params["name"]; has {
				g.Name = fmt.Sprintf("%v", name)
			}
		}
		s.mu.Unlock()
		if !ok {
			http.Error(w, "does not exist", http.StatusInternalServerError)
			return
		}
		s.finishTask(w, m[1], "update")
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	m := rxCreate.FindStringSubmatch(r.URL.Path)
	node, typ := m[1], m[2]
	params := formParams(r)

	vmid, err := strconv.Atoi(fmt.Sprintf("%v", params["vmid"]))
	if err != nil {
		http.Error(w, "invalid vmid", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if _, exists := s.guests[vmid]; exists {
		s.mu.Unlock()
		http.Error(w, fmt.Sprintf("unable to create VM %d - VM %d already exists", vmid, vmid), http.StatusInternalServerError)
		return
	}
	config := map[string]any{}
	for k, v := range params {
		if k != "vmid" {
			config[k] = v
		}
	}
	s.guests[vmid] = &Guest{
		VMID:   vmid,
		Name:   fmt.Sprintf("%v", params["name"]),
		Node:   node,
		Type:   typ,
		Status: "stopped",
		Config: config,
	}
	if vmid >= s.nextID {
		s.nextID = vmid + 1
	}
	s.mu.Unlock()

	s.finishTask(w, node, "create")
}

func (s *Server) handleClone(w http.ResponseWriter, r *http.Request) {
	m := rxClone.FindStringSubmatch(r.URL.Path)
	node := m[1]
	srcID, _ := strconv.Atoi(m[3])
	params := formParams(r)

	newID, err := strconv.Atoi(fmt.Sprintf("%v", params["newid"]))
	if err != nil {
		http.Error(w, "invalid newid", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	src, ok := s.guests[srcID]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "source does not exist", http.StatusInternalServerError)
		return
	}
	config := map[string]any{}
	for k, v := range src.Config {
		config[k] = v
	}
	target := node
	if t, has := params["target"]; has {
		target = fmt.Sprintf("%v", t)
	}
	name := src.Name + "-clone"
	if n, has := params["name"]; has {
		name = fmt.Sprintf("%v", n)
	}
	config["name"] = name
	s.guests[newID] = &Guest{
		VMID:   newID,
		Name:   name,
		Node:   target,
		Type:   src.Type,
		Status: "stopped",
		Config: config,
	}
	if newID >= s.nextID {
		s.nextID = newID + 1
	}
	s.mu.Unlock()

	s.finishTask(w, node, "clone")
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	m := rxGuest.FindStringSubmatch(r.URL.Path)
	vmid, _ := strconv.Atoi(m[3])

	s.mu.Lock()
	_, ok := s.guests[vmid]
	delete(s.guests, vmid)
	s.mu.Unlock()
	if !ok {
		http.Error(w, "does not exist", http.StatusInternalServerError)
		return
	}
	s.finishTask(w, m[1], "delete")
}

func (s *Server) handleLifecycle(w http.ResponseWriter, r *http.Request) {
	m := rxStatus.FindStringSubmatch(r.URL.Path)
	vmid, _ := strconv.Atoi(m[3])
	op := m[4]

	s.mu.Lock()
	g, ok := s.guests[vmid]
	if ok {
		if op == "start" {
			g.Status = "running"
		} else {
			g.Status = "stopped"
		}
	}
	s.mu.Unlock()
	if !ok {
		http.Error(w, "does not exist", http.StatusInternalServerError)
		return
	}
	s.finishTask(w, m[1], op)
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	m := rxTask.FindStringSubmatch(r.URL.Path)
	upid := m[2]

	s.mu.Lock()
	exit, ok := s.taskResults[upid]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "no such task", http.StatusInternalServerError)
		return
	}
	writeData(w, map[string]any{"status": "stopped", "exitstatus": exit})
}

// finishTask allocates a UPID whose result is immediately available and
// writes it as the response body, mimicking an async PVE task.
func (s *Server) finishTask(w http.ResponseWriter, node, op string) {
	s.mu.Lock()
	s.taskSeq++
	upid := fmt.Sprintf("UPID:%s:%08X:00000000:00000000:%s:0:fake@pam:", node, s.taskSeq, op)
	exit := "OK"
	if failure, has := s.failNext[op]; has {
		exit = failure
		delete(s.failNext, op)
	}
	s.taskResults[upid] = exit
	s.mu.Unlock()
	writeData(w, upid)
}

func formParams(r *http.Request) map[string]any {
	params := map[string]any{}
	if err := r.ParseForm(); err != nil {
		return params
	}
	for k := range r.PostForm {
		params[k] = r.PostForm.Get(k)
	}
	return params
}

func writeData(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
