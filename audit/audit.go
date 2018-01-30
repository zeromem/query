//  Copyright (c) 2017 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package audit

import (
	"fmt"
	"strings"
	"time"

	adt "github.com/couchbase/goutils/go-cbaudit"
	"github.com/couchbase/query/logging"
)

type Auditable interface {
	// Standard fields used for all audit records.
	EventGenericFields() adt.GenericFields

	// success/fatal/stopped/etc.
	EventStatus() string

	// The N1QL statement executed.
	Statement() string

	// Statement id.
	EventId() string

	// Event type. eg. "SELECT", "DELETE", "PREPARE"
	EventType() string

	// User ids submitted with request. eg. ["kirk", "spock"]
	EventUsers() []string

	// The User-Agent string from the request. This is used to identify the type of client
	// that sent the request (SDK, QWB, CBQ, ...)
	UserAgent() string

	// Event server name.
	EventNodeName() string

	// Query parameters.
	EventNamedArgs() map[string]string
	EventPositionalArgs() []string

	// From client_context_id input parameter.
	// Useful for separating system-generated queries from user-issued queries.
	ClientContextId() string

	IsAdHoc() bool

	// Metrics
	ElapsedTime() time.Duration
	ExecutionTime() time.Duration
	EventResultCount() int
	EventResultSize() int
	MutationCount() uint64
	SortCount() uint64
	EventErrorCount() int
	EventWarningCount() int
}

type ApiAuditFields struct {
	GenericFields  adt.GenericFields
	EventTypeId    uint32
	Users          []string
	HttpMethod     string
	HttpResultCode int
	ErrorCode      int
	ErrorMessage   string

	Stat    string
	Name    string
	Request string
	Cluster string
	Node    string
	Values  interface{}
	Body    interface{}
}

// An auditor is a component that can accept an audit record for processing.
// We create a formal interface, so we can have two Auditors: the regular one that
// talks to the audit daemon, and a mock that just stores audit records for testing.
// The mock is over in the test file.
type Auditor interface {
	// Should we contact the audit demon at all?
	doAudit() bool

	// Some users are trusted, so their actions do not need to be audited.
	// Is this action from one such user?
	userIsWhitelisted(userId string) bool

	// Some events are disabled, and do not need to be audited.
	// Is this one of them?
	eventIsDisabled(uint32) bool

	// In normal processing, we want the call to submit the audit record to
	// the audit daemon done offline, in a goroutine of its own.
	// But that makes testing difficult, so we do the submission inline
	// when testing.
	submitInline() bool

	submit(eventId uint32, event *n1qlAuditEvent) error

	submitApiRequest(eventId uint32, event *n1qlAuditApiRequestEvent) error
}

type standardAuditor struct {
	auditService *adt.AuditSvc
}

func (sa *standardAuditor) submitInline() bool {
	return false
}

func (sa *standardAuditor) submit(eventId uint32, event *n1qlAuditEvent) error {
	return sa.auditService.Write(eventId, *event)
}

func (sa *standardAuditor) submitApiRequest(eventId uint32, event *n1qlAuditApiRequestEvent) error {
	return sa.auditService.Write(eventId, *event)
}

func (sa *standardAuditor) userIsWhitelisted(user string) bool {
	// TODO
	return false
}

func (sa *standardAuditor) eventIsDisabled(eventId uint32) bool {
	// No real event number?
	if eventId == API_DO_NOT_AUDIT {
		return true
	}

	// TODO
	if eventId == API_ADMIN_STATS {
		// The /admin/stats API gets a lot of requests.
		// Disable them for now so the log doesn't get too crowded.
		return true
	}
	return false
}

var _AUDITOR Auditor

func StartAuditService(server string) {
	var err error
	service, err := adt.NewAuditSvc(server)
	if err == nil {
		_AUDITOR = &standardAuditor{auditService: service}
		logging.Infof("Audit service started.")
	} else {
		logging.Errorf("Audit service not started: %v", err)
	}
}

// Event types are described in /query/etc/audit_descriptor.json
var _EVENT_TYPE_MAP = map[string]uint32{
	"SELECT":               28672,
	"EXPLAIN":              28673,
	"PREPARE":              28674,
	"INFER":                28675,
	"INSERT":               28676,
	"UPSERT":               28677,
	"DELETE":               28678,
	"UPDATE":               28679,
	"MERGE":                28680,
	"CREATE_INDEX":         28681,
	"DROP_INDEX":           28682,
	"ALTER_INDEX":          28683,
	"BUILD_INDEX":          28684,
	"GRANT_ROLE":           28685,
	"REVOKE_ROLE":          28686,
	"CREATE_PRIMARY_INDEX": 28688,
}

func Submit(event Auditable) {
	if _AUDITOR == nil {
		return // Nothing configured. Nothing to be done.
	}

	if !_AUDITOR.doAudit() {
		return
	}

	eventType := event.EventType()
	eventTypeId := _EVENT_TYPE_MAP[eventType]

	// Handle unrecognized events.
	if eventTypeId == 0 {
		eventTypeId = 28687
	}

	if _AUDITOR.eventIsDisabled(eventTypeId) {
		return
	}

	// We build the audit record from the request in the main thread
	// because the request will be destroyed soon after the call to Submit(),
	// and we don't want to cause a race condition.
	auditRecords := buildAuditRecords(event)
	for _, record := range auditRecords {
		if _AUDITOR.submitInline() {
			submitForAudit(eventTypeId, record)
		} else {
			go submitForAudit(eventTypeId, record)
		}
	}
}

const (
	API_DO_NOT_AUDIT                     = 0
	API_ADMIN_STATS                      = 28689
	API_ADMIN_VITALS                     = 28690
	API_ADMIN_PREPAREDS                  = 28691
	API_ADMIN_ACTIVE_REQUESTS            = 28692
	API_ADMIN_INDEXES_PREPAREDS          = 28693
	API_ADMIN_INDEXES_ACTIVE_REQUESTS    = 28694
	API_ADMIN_INDEXES_COMPLETED_REQUESTS = 28695
	API_ADMIN_PING                       = 28697
	API_ADMIN_CONFIG                     = 28698
	API_ADMIN_SSL_CERT                   = 28699
	API_ADMIN_SETTINGS                   = 28700
	API_ADMIN_CLUSTERS                   = 28701
	API_ADMIN_COMPLETED_REQUESTS         = 28702
)

func SubmitApiRequest(event *ApiAuditFields) {
	if _AUDITOR == nil {
		return // Nothing configured. Nothing to be done.
	}

	if !_AUDITOR.doAudit() {
		return
	}

	eventTypeId := event.EventTypeId

	if _AUDITOR.eventIsDisabled(eventTypeId) {
		return
	}

	// We build the audit record from the request in the main thread
	// because the request will be destroyed soon after the call to Submit(),
	// and we don't want to cause a race condition.
	auditRecords := buildApiRequestAuditRecords(event)
	for _, record := range auditRecords {
		if _AUDITOR.submitInline() {
			submitApiRequestForAudit(eventTypeId, record)
		} else {
			go submitApiRequestForAudit(eventTypeId, record)
		}
	}
}

// Returns a list of audit records, because each user credential submitted as part of
// the requests generates a separate audit record.
func buildAuditRecords(event Auditable) []*n1qlAuditEvent {
	// Grab the data from the event, so we don't query the duplicated data
	// multiple times.
	genericFields := event.EventGenericFields()
	requestId := event.EventId()
	statement := event.Statement()
	namedArgs := event.EventNamedArgs()
	positionalArgs := event.EventPositionalArgs()
	clientContextId := event.ClientContextId()
	isAdHoc := event.IsAdHoc()
	userAgent := event.UserAgent()
	node := event.EventNodeName()
	status := event.EventStatus()
	metrics := &n1qlMetrics{
		ElapsedTime:   fmt.Sprintf("%v", event.ElapsedTime()),
		ExecutionTime: fmt.Sprintf("%v", event.ExecutionTime()),
		ResultCount:   event.EventResultCount(),
		ResultSize:    event.EventResultSize(),
		MutationCount: event.MutationCount(),
		SortCount:     event.SortCount(),
		ErrorCount:    event.EventErrorCount(),
		WarningCount:  event.EventWarningCount(),
	}

	// No credentials at all? Generate one record.
	users := event.EventUsers()
	if len(users) == 0 {
		record := &n1qlAuditEvent{
			GenericFields:   genericFields,
			RequestId:       requestId,
			Statement:       statement,
			NamedArgs:       namedArgs,
			PositionalArgs:  positionalArgs,
			ClientContextId: clientContextId,
			IsAdHoc:         isAdHoc,
			UserAgent:       userAgent,
			Node:            node,
			Status:          status,
			Metrics:         metrics,
		}
		return []*n1qlAuditEvent{record}
	}

	// Figure out which users to generate events for.
	auditableUsers := make([]string, 0, len(users))
	for _, user := range users {
		if !_AUDITOR.userIsWhitelisted(user) {
			auditableUsers = append(auditableUsers, user)
		}
	}

	// Generate one record per user.
	records := make([]*n1qlAuditEvent, len(auditableUsers))
	for i, user := range auditableUsers {
		record := &n1qlAuditEvent{
			GenericFields:   genericFields,
			RequestId:       requestId,
			Statement:       statement,
			NamedArgs:       namedArgs,
			PositionalArgs:  positionalArgs,
			ClientContextId: clientContextId,
			IsAdHoc:         isAdHoc,
			UserAgent:       userAgent,
			Node:            node,
			Status:          status,
			Metrics:         metrics,
		}
		source := "local"
		userName := user
		// Handle non-local users, e.g. "external:dtrump"
		if strings.Contains(user, ":") {
			parts := strings.SplitN(user, ":", 2)
			source = parts[0]
			userName = parts[1]
		}
		record.GenericFields.RealUserid.Source = source
		record.GenericFields.RealUserid.Username = userName

		records[i] = record
	}
	return records
}

// Returns a list of audit records, because each user credential submitted as part of
// the requests generates a separate audit record.
func buildApiRequestAuditRecords(event *ApiAuditFields) []*n1qlAuditApiRequestEvent {
	// No credentials at all? Generate one record.
	users := event.Users
	if len(users) == 0 {
		record := &n1qlAuditApiRequestEvent{
			GenericFields:  event.GenericFields,
			HttpMethod:     event.HttpMethod,
			HttpResultCode: event.HttpResultCode,
			ErrorCode:      event.ErrorCode,
			ErrorMessage:   event.ErrorMessage,
			Stat:           event.Stat,
			Name:           event.Name,
			Request:        event.Request,
			Values:         event.Values,
			Cluster:        event.Cluster,
			Node:           event.Node,
			Body:           event.Body,
		}
		return []*n1qlAuditApiRequestEvent{record}
	}

	// Figure out which users to generate events for.
	auditableUsers := make([]string, 0, len(users))
	for _, user := range users {
		if !_AUDITOR.userIsWhitelisted(user) {
			auditableUsers = append(auditableUsers, user)
		}
	}

	// Generate one record per user.
	records := make([]*n1qlAuditApiRequestEvent, len(auditableUsers))
	for i, user := range auditableUsers {
		record := &n1qlAuditApiRequestEvent{
			GenericFields:  event.GenericFields,
			HttpMethod:     event.HttpMethod,
			HttpResultCode: event.HttpResultCode,
			ErrorCode:      event.ErrorCode,
			ErrorMessage:   event.ErrorMessage,
			Stat:           event.Stat,
			Name:           event.Name,
			Request:        event.Request,
			Values:         event.Values,
			Cluster:        event.Cluster,
			Node:           event.Node,
			Body:           event.Body,
		}
		source := "local"
		userName := user
		// Handle non-local users, e.g. "external:dtrump"
		if strings.Contains(user, ":") {
			parts := strings.SplitN(user, ":", 2)
			source = parts[0]
			userName = parts[1]
		}
		record.GenericFields.RealUserid.Source = source
		record.GenericFields.RealUserid.Username = userName

		records[i] = record
	}
	return records
}

func submitForAudit(eventId uint32, auditRecord *n1qlAuditEvent) {
	err := _AUDITOR.submit(eventId, auditRecord)
	if err != nil {
		logging.Errorf("Unable to submit event %+v for audit: %v", *auditRecord, err)
	}
}

func submitApiRequestForAudit(eventId uint32, auditRecord *n1qlAuditApiRequestEvent) {
	err := _AUDITOR.submitApiRequest(eventId, auditRecord)
	if err != nil {
		logging.Errorf("Unable to submit API request event %+v for audit: %v", *auditRecord, err)
	}
}

// If possible, use whatever field names are used elsewhere in the N1QL system.
// Follow whatever naming scheme (under_scores/camelCase/What.Ever) is standard for each field.
// If no standard exists for the field, use camelCase.
type n1qlAuditEvent struct {
	adt.GenericFields

	RequestId       string            `json:"requestId"`
	Statement       string            `json:"statement"`
	NamedArgs       map[string]string `json:"namedArgs,omitempty"`
	PositionalArgs  []string          `json:"positionalArgs,omitempty"`
	ClientContextId string            `json:"clientContextId,omitempty"`

	IsAdHoc   bool   `json:"isAdHoc"`
	UserAgent string `json:"userAgent"`
	Node      string `json:"node"`

	Status string `json:"status"`

	Metrics *n1qlMetrics `json:"metrics"`
}

type n1qlMetrics struct {
	ElapsedTime   string `json:"elapsedTime"`
	ExecutionTime string `json:"executionTime"`
	ResultCount   int    `json:"resultCount"`
	ResultSize    int    `json:"resultSize"`
	MutationCount uint64 `json:"mutationCount,omitempty"`
	SortCount     uint64 `json:"sortCount,omitempty"`
	ErrorCount    int    `json:"errorCount,omitempty"`
	WarningCount  int    `json:"warningCount,omitempty"`
}

type n1qlAuditApiRequestEvent struct {
	adt.GenericFields

	HttpMethod     string `json:"httpMethod"`
	HttpResultCode int    `json:"httpResultCode"`
	ErrorCode      int    `json:"errorCode,omitempty"`
	ErrorMessage   string `json:"errorMessage",omitempty"`

	Stat    string      `json:"stat,omitempty"`
	Name    string      `json:"name,omitempty"`
	Request string      `json:"request,omitempty"`
	Cluster string      `json:"cluster,omitempty"`
	Node    string      `json:"node,omitempty"`
	Values  interface{} `json:"values,omitempty"`
	Body    interface{} `json:"body,omitempty"`
}