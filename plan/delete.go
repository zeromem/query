//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package plan

import (
	"encoding/json"

	"github.com/couchbase/query/datastore"
	"github.com/couchbase/query/expression"
)

type SendDelete struct {
	readwrite
	keyspace datastore.Keyspace
	alias    string
	limit    expression.Expression
}

func NewSendDelete(keyspace datastore.Keyspace, alias string, limit expression.Expression) *SendDelete {
	return &SendDelete{
		keyspace: keyspace,
		alias:    alias,
		limit:    limit,
	}
}

func (this *SendDelete) Accept(visitor Visitor) (interface{}, error) {
	return visitor.VisitSendDelete(this)
}

func (this *SendDelete) Keyspace() datastore.Keyspace {
	return this.keyspace
}

func (this *SendDelete) Alias() string {
	return this.alias
}

func (this *SendDelete) Limit() expression.Expression {
	return this.limit
}

func (this *SendDelete) MarshalJSON() ([]byte, error) {
	r := map[string]interface{}{"#operator": "SendDelete"}
	r["namespace"] = this.keyspace.NamespaceId()
	r["keyspace"] = this.keyspace.Name()
	r["alias"] = this.alias
	return json.Marshal(r)
}

func (this *SendDelete) New() Operator {
	return &SendDelete{}
}

func (this *SendDelete) UnmarshalJSON(body []byte) error {
	var _unmarshalled struct {
		_     string `json:"#operator"`
		Names string `json:"namespace"`
		Keys  string `json:"keyspace"`
		Alias string `json:"alias"`
	}

	err := json.Unmarshal(body, &_unmarshalled)
	if err != nil {
		return err
	}

	this.alias = _unmarshalled.Alias
	this.keyspace, err = datastore.GetKeyspace(_unmarshalled.Names, _unmarshalled.Keys)

	return err
}
