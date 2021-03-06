// Copyright 2012, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vtgate

import (
	"reflect"
	"testing"
	"time"

	tproto "github.com/youtube/vitess/go/vt/tabletserver/proto"
	"github.com/youtube/vitess/go/vt/topo"
	"github.com/youtube/vitess/go/vt/vtgate/proto"
)

// This file uses the sandbox_test framework.

func init() {
	Init(new(sandboxTopo), "aa", 1*time.Second, 10, 1*time.Millisecond)
}

func TestVTGateExecuteShard(t *testing.T) {
	resetSandbox()
	sbc := &sandboxConn{}
	testConns[0] = sbc
	q := proto.QueryShard{
		Sql:    "query",
		Shards: []string{"0"},
	}
	qr := new(proto.QueryResult)
	err := RpcVTGate.ExecuteShard(nil, &q, qr)
	if err != nil {
		t.Errorf("want nil, got %v", err)
	}
	wantqr := new(proto.QueryResult)
	proto.PopulateQueryResult(singleRowResult, wantqr)
	if !reflect.DeepEqual(wantqr, qr) {
		t.Errorf("want \n%#v, got \n%#v", singleRowResult, qr)
	}
	if qr.Session != nil {
		t.Errorf("want nil, got %#v\n", qr.Session)
	}

	q.Session = new(proto.Session)
	RpcVTGate.Begin(nil, q.Session)
	if !q.Session.InTransaction {
		t.Errorf("want true, got false")
	}
	RpcVTGate.ExecuteShard(nil, &q, qr)
	wantSession := &proto.Session{
		InTransaction: true,
		ShardSessions: []*proto.ShardSession{{
			Shard:         "0",
			TransactionId: 1,
		}},
	}
	if !reflect.DeepEqual(wantSession, q.Session) {
		t.Errorf("want \n%#v, got \n%#v", wantSession, q.Session)
	}

	RpcVTGate.Commit(nil, q.Session)
	if sbc.CommitCount != 1 {
		t.Errorf("want 1, got %d", sbc.CommitCount)
	}

	q.Session = new(proto.Session)
	RpcVTGate.Begin(nil, q.Session)
	RpcVTGate.ExecuteShard(nil, &q, qr)
	RpcVTGate.Rollback(nil, q.Session)
	/*
		// Flaky: This test should be run manually.
		runtime.Gosched()
		if sbc.RollbackCount != 1 {
			t.Errorf("want 1, got %d", sbc.RollbackCount)
		}
	*/
}

func TestVTGateExecuteBatchShard(t *testing.T) {
	resetSandbox()
	mapTestConn("-20", &sandboxConn{})
	mapTestConn("20-40", &sandboxConn{})
	q := proto.BatchQueryShard{
		Queries: []tproto.BoundQuery{{
			"query",
			nil,
		}, {
			"query",
			nil,
		}},
		Shards: []string{"-20", "20-40"},
	}
	qrl := new(proto.QueryResultList)
	err := RpcVTGate.ExecuteBatchShard(nil, &q, qrl)
	if err != nil {
		t.Errorf("want nil, got %v", err)
	}
	if len(qrl.List) != 2 {
		t.Errorf("want 2, got %v", len(qrl.List))
	}
	if qrl.List[0].RowsAffected != 2 {
		t.Errorf("want 2, got %v", qrl.List[0].RowsAffected)
	}
	if qrl.Session != nil {
		t.Errorf("want nil, got %#v\n", qrl.Session)
	}

	q.Session = new(proto.Session)
	RpcVTGate.Begin(nil, q.Session)
	err = RpcVTGate.ExecuteBatchShard(nil, &q, qrl)
	if len(q.Session.ShardSessions) != 2 {
		t.Errorf("want 2, got %d", len(q.Session.ShardSessions))
	}
}

func TestVTGateStreamExecuteKeyRange(t *testing.T) {
	resetSandbox()
	sbc := &sandboxConn{}
	mapTestConn("-20", sbc)
	sq := proto.StreamQueryKeyRange{
		Sql:        "query",
		KeyRange:   "-20",
		TabletType: topo.TYPE_MASTER,
	}
	// Test for successful execution
	var qrs []*proto.QueryResult
	err := RpcVTGate.StreamExecuteKeyRange(nil, &sq, func(r *proto.QueryResult) error {
		qrs = append(qrs, r)
		return nil
	})
	if err != nil {
		t.Errorf("want nil, got %v", err)
	}
	row := new(proto.QueryResult)
	proto.PopulateQueryResult(singleRowResult, row)
	want := []*proto.QueryResult{row}
	if !reflect.DeepEqual(want, qrs) {
		t.Errorf("want \n%#v, got \n%#v", want, qrs)
	}

	sq.Session = new(proto.Session)
	qrs = nil
	RpcVTGate.Begin(nil, sq.Session)
	err = RpcVTGate.StreamExecuteKeyRange(nil, &sq, func(r *proto.QueryResult) error {
		qrs = append(qrs, r)
		return nil
	})
	want = []*proto.QueryResult{
		row,
		&proto.QueryResult{
			Session: &proto.Session{
				InTransaction: true,
				ShardSessions: []*proto.ShardSession{{
					Shard:         "-20",
					TransactionId: 1,
					TabletType:    topo.TYPE_MASTER,
				}},
			},
		},
	}
	if !reflect.DeepEqual(want, qrs) {
		t.Errorf("want \n%#v, got \n%#v", want, qrs)
	}

	// Test for error condition - multiple shards
	sq.KeyRange = "10-40"
	err = RpcVTGate.StreamExecuteKeyRange(nil, &sq, func(r *proto.QueryResult) error {
		qrs = append(qrs, r)
		return nil
	})
	if err == nil {
		t.Errorf("want not nil, got %v", err)
	}
	// Test for error condition - multiple shards, non-partial keyspace
	sq.KeyRange = ""
	err = RpcVTGate.StreamExecuteKeyRange(nil, &sq, func(r *proto.QueryResult) error {
		qrs = append(qrs, r)
		return nil
	})
	if err == nil {
		t.Errorf("want not nil, got %v", err)
	}
}

func TestVTGateStreamExecuteShard(t *testing.T) {
	resetSandbox()
	sbc := &sandboxConn{}
	testConns[0] = sbc
	q := proto.QueryShard{
		Sql:        "query",
		Shards:     []string{"0"},
		TabletType: topo.TYPE_MASTER,
	}
	// Test for successful execution
	var qrs []*proto.QueryResult
	err := RpcVTGate.StreamExecuteShard(nil, &q, func(r *proto.QueryResult) error {
		qrs = append(qrs, r)
		return nil
	})
	if err != nil {
		t.Errorf("want nil, got %v", err)
	}
	row := new(proto.QueryResult)
	proto.PopulateQueryResult(singleRowResult, row)
	want := []*proto.QueryResult{row}
	if !reflect.DeepEqual(want, qrs) {
		t.Errorf("want \n%#v, got \n%#v", want, qrs)
	}

	q.Session = new(proto.Session)
	qrs = nil
	RpcVTGate.Begin(nil, q.Session)
	err = RpcVTGate.StreamExecuteShard(nil, &q, func(r *proto.QueryResult) error {
		qrs = append(qrs, r)
		return nil
	})
	want = []*proto.QueryResult{
		row,
		&proto.QueryResult{
			Session: &proto.Session{
				InTransaction: true,
				ShardSessions: []*proto.ShardSession{{
					Shard:         "0",
					TransactionId: 1,
					TabletType:    topo.TYPE_MASTER,
				}},
			},
		},
	}
	if !reflect.DeepEqual(want, qrs) {
		t.Errorf("want \n%#v, got \n%#v", want, qrs)
	}

}
