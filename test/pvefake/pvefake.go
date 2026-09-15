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
	"slices"
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

	exec         ExecScript
	execRuns     map[int]int // pid -> remaining "still running" reads
	execPID      int
	execCommands [][]string
	agentError   string

	requests []string // "METHOD /path", in arrival order
}

// ExecScript programs how the fake's QEMU guest agent behaves. The zero
// value is a command that exits immediately with status 0.
type ExecScript struct {
	ExitCode int
	Stdout   string
	Stderr   string
	// PollsBeforeExit reports the command as still running for that many
	// exec-status reads before it reports the exit code.
	PollsBeforeExit int
	// NeverExits makes exec-status always report the command as running,
	// which is what a client-side timeout has to cope with.
	NeverExits bool
}

// New starts a fake server. Call Close when done.
func New() *Server {
	s := &Server{
		guests:      map[int]*Guest{},
		nextID:      100,
		failNext:    map[string]string{},
		taskResults: map[string]string{},
		execRuns:    map[int]int{},
		execPID:     1000,
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

// SetExec programs the guest agent's answers for subsequent
// /agent/exec calls.
func (s *Server) SetExec(script ExecScript) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exec = script
}

// SetAgentUnavailable makes the agent endpoints fail the way Proxmox VE
// does when the guest agent is switched off or not running. An empty
// message clears the failure.
func (s *Server) SetAgentUnavailable(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentError = message
}

// ExecCommands returns every command the guest agent was asked to run,
// in order, so tests can assert the endpoint was actually reached.
func (s *Server) ExecCommands() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]string, len(s.execCommands))
	copy(out, s.execCommands)
	return out
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
	rxResize = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/(qemu|lxc)/(\d+)/resize$`)
	rxMigr   = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/(qemu|lxc)/(\d+)/migrate$`)
	rxExec   = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/qemu/(\d+)/agent/exec$`)
	rxExecSt = regexp.MustCompile(`^/api2/json/nodes/([^/]+)/qemu/(\d+)/agent/exec-status$`)
	// rxDiskKey matches the config keys that carry a disk volume.
	rxDiskKey = regexp.MustCompile(`^(scsi|virtio|sata|ide)\d+$`)
)

// Requests returns every request the fake handled, as "METHOD /path",
// so a test can assert which endpoint a code path actually reached.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// SawRequest reports whether any handled request matches "METHOD /path".
func (s *Server) SawRequest(method, path string) bool {
	return slices.Contains(s.Requests(), method+" "+path)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	s.mu.Unlock()

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
	case rxResize.MatchString(path) && r.Method == http.MethodPut:
		s.handleResize(w, r)
	case rxMigr.MatchString(path) && r.Method == http.MethodPost:
		s.handleMigrate(w, r)
	case rxExec.MatchString(path) && r.Method == http.MethodPost:
		s.handleAgentExec(w, r)
	case rxExecSt.MatchString(path) && r.Method == http.MethodGet:
		s.handleAgentExecStatus(w, r)
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
			materializeDisks(vmid, g.Config)
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
	materializeDisks(vmid, config)
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

func (s *Server) handleResize(w http.ResponseWriter, r *http.Request) {
	m := rxResize.FindStringSubmatch(r.URL.Path)
	vmid, _ := strconv.Atoi(m[3])
	params := formParams(r)
	disk := fmt.Sprintf("%v", params["disk"])
	size := fmt.Sprintf("%v", params["size"])

	s.mu.Lock()
	g, ok := s.guests[vmid]
	var failure string
	switch {
	case !ok:
		failure = "does not exist"
	default:
		current, has := g.Config[disk]
		if !has {
			failure = fmt.Sprintf("disk '%s' does not exist", disk)
			break
		}
		resized, err := resizeDiskValue(fmt.Sprintf("%v", current), size)
		if err != nil {
			failure = err.Error()
			break
		}
		g.Config[disk] = resized
	}
	s.mu.Unlock()

	if failure != "" {
		http.Error(w, failure, http.StatusInternalServerError)
		return
	}
	s.finishTask(w, m[1], "resize")
}

func (s *Server) handleMigrate(w http.ResponseWriter, r *http.Request) {
	m := rxMigr.FindStringSubmatch(r.URL.Path)
	vmid, _ := strconv.Atoi(m[3])
	params := formParams(r)
	target := fmt.Sprintf("%v", params["target"])

	s.mu.Lock()
	g, ok := s.guests[vmid]
	if ok && target != "" {
		g.Node = target
	}
	s.mu.Unlock()

	if !ok {
		http.Error(w, "does not exist", http.StatusInternalServerError)
		return
	}
	if target == "" {
		http.Error(w, "missing parameter 'target'", http.StatusBadRequest)
		return
	}
	s.finishTask(w, m[1], "migrate")
}

// handleAgentExec starts a "command" and answers with its pid. Unlike
// most mutating endpoints this one is not a task: the caller polls
// exec-status instead.
func (s *Server) handleAgentExec(w http.ResponseWriter, r *http.Request) {
	m := rxExec.FindStringSubmatch(r.URL.Path)
	vmid, _ := strconv.Atoi(m[2])

	command := []string{}
	if err := r.ParseForm(); err == nil {
		command = append(command, r.PostForm["command"]...)
	}

	s.mu.Lock()
	if s.agentError != "" {
		message := s.agentError
		s.mu.Unlock()
		writeAPIError(w, message)
		return
	}
	if _, ok := s.guests[vmid]; !ok {
		s.mu.Unlock()
		http.Error(w, "does not exist", http.StatusInternalServerError)
		return
	}
	s.execCommands = append(s.execCommands, command)
	s.execPID++
	pid := s.execPID
	s.execRuns[pid] = s.exec.PollsBeforeExit
	s.mu.Unlock()

	writeData(w, map[string]any{"pid": float64(pid)})
}

func (s *Server) handleAgentExecStatus(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.Atoi(r.URL.Query().Get("pid"))
	if err != nil {
		http.Error(w, "missing parameter 'pid'", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	message := s.agentError
	remaining, known := s.execRuns[pid]
	running := s.exec.NeverExits || remaining > 0
	if remaining > 0 {
		s.execRuns[pid] = remaining - 1
	}
	script := s.exec
	s.mu.Unlock()

	switch {
	case message != "":
		writeAPIError(w, message)
	case !known:
		http.Error(w, fmt.Sprintf("no such process %d", pid), http.StatusInternalServerError)
	case running:
		writeData(w, map[string]any{"exited": float64(0)})
	default:
		writeData(w, map[string]any{
			"exited":   float64(1),
			"exitcode": float64(script.ExitCode),
			"out-data": script.Stdout,
			"err-data": script.Stderr,
		})
	}
}

// writeAPIError answers the way Proxmox VE does on a failed call: a
// JSON body with a "message", which the SDK surfaces verbatim.
func writeAPIError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": nil, "message": message})
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

// materializeDisks rewrites the allocation form a manifest sends
// ("local-lvm:16,discard=on") into the volume form Proxmox VE stores and
// reads back ("local-lvm:vm-200-disk-0,discard=on,size=16G"). Without
// this the fake would echo the request verbatim and resize would have no
// "size=" to grow.
func materializeDisks(vmid int, config map[string]any) {
	for key, raw := range config {
		if !rxDiskKey.MatchString(key) {
			continue
		}
		value := fmt.Sprintf("%v", raw)
		head, opts, _ := strings.Cut(value, ",")
		storage, request, ok := strings.Cut(head, ":")
		if !ok {
			continue
		}
		gigabytes, err := strconv.ParseFloat(request, 64)
		if err != nil {
			continue // already a volume name
		}
		slot := strings.TrimLeft(key, "abcdefghijklmnopqrstuvwxyz")
		rebuilt := fmt.Sprintf("%s:vm-%d-disk-%s", storage, vmid, slot)
		if opts != "" {
			rebuilt += "," + opts
		}
		config[key] = rebuilt + fmt.Sprintf(",size=%sG", strconv.FormatFloat(gigabytes, 'f', -1, 64))
	}
}

// resizeDiskValue applies a /resize request to a stored disk value.
// A leading "+" grows the disk, anything else sets it outright; Proxmox
// VE refuses to shrink, and so does this.
func resizeDiskValue(value, size string) (string, error) {
	if size == "" {
		return "", fmt.Errorf("missing parameter 'size'")
	}
	parts := strings.Split(value, ",")
	currentIdx := -1
	var current int64
	for i, part := range parts {
		if raw, ok := strings.CutPrefix(part, "size="); ok {
			currentIdx = i
			current = sizeBytes(raw)
		}
	}
	if currentIdx < 0 {
		return "", fmt.Errorf("disk has no size to resize")
	}

	wanted := sizeBytes(strings.TrimPrefix(size, "+"))
	if wanted <= 0 {
		return "", fmt.Errorf("unable to parse size '%s'", size)
	}
	if strings.HasPrefix(size, "+") {
		wanted += current
	}
	if wanted < current {
		return "", fmt.Errorf("shrinking disks is not supported")
	}

	parts[currentIdx] = "size=" + sizeString(wanted)
	return strings.Join(parts, ","), nil
}

// sizeBytes converts "32G"/"512M"/"32" (GB) to bytes; 0 on nonsense.
func sizeBytes(s string) int64 {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1) << 30 // a bare number means GB
	if len(s) > 0 {
		switch s[len(s)-1] {
		case 'K':
			mult, s = 1<<10, s[:len(s)-1]
		case 'M':
			mult, s = 1<<20, s[:len(s)-1]
		case 'G':
			mult, s = 1<<30, s[:len(s)-1]
		case 'T':
			mult, s = 1<<40, s[:len(s)-1]
		}
	}
	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(value * float64(mult))
}

// sizeString renders bytes the way Proxmox VE writes them back, using
// the largest unit that divides evenly.
func sizeString(bytes int64) string {
	units := []struct {
		suffix string
		size   int64
	}{{"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}}
	for _, u := range units {
		if bytes%u.size == 0 {
			return strconv.FormatInt(bytes/u.size, 10) + u.suffix
		}
	}
	return strconv.FormatInt(bytes, 10)
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
