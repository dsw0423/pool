// Copyright 2019 shimingyah. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// ee the License for the specific language governing permissions and
// limitations under the License.

package pool

import (
	"flag"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

var endpoint = flag.String("endpoint", "127.0.0.1:8080", "grpc server endpoint")

func newPool(op *Options) (Pool, *pool, Options, error) {
	opt := DefaultOptions
	opt.Dial = DialTest
	if op != nil {
		opt = *op
	}
	p, err := New(*endpoint, opt)
	return p, p.(*pool), opt, err
}

func TestNew(t *testing.T) {
	p, nativePool, opt, err := newPool(nil)
	require.NoError(t, err)
	defer p.Close()

	require.EqualValues(t, 0, nativePool.index)
	require.EqualValues(t, 0, nativePool.ref)
	require.EqualValues(t, opt.MaxIdle, nativePool.current)
	require.EqualValues(t, opt.MaxActive, len(nativePool.conns))
}

func TestClose(t *testing.T) {
	p, nativePool, opt, err := newPool(nil)
	require.NoError(t, err)
	p.Close()

	require.EqualValues(t, 0, nativePool.index)
	require.EqualValues(t, 0, nativePool.ref)
	require.EqualValues(t, 0, nativePool.current)
	require.EqualValues(t, true, nativePool.conns[0] == nil)
	require.EqualValues(t, true, nativePool.conns[opt.MaxIdle-1] == nil)
}

func TestReset(t *testing.T) {
	p, nativePool, opt, err := newPool(nil)
	require.NoError(t, err)
	defer p.Close()

	nativePool.reset(0)
	require.EqualValues(t, true, nativePool.conns[0] == nil)
	nativePool.reset(opt.MaxIdle + 1)
	require.EqualValues(t, true, nativePool.conns[opt.MaxIdle+1] == nil)
}

func TestBasicGet(t *testing.T) {
	p, nativePool, _, err := newPool(nil)
	require.NoError(t, err)
	defer p.Close()

	conn, err := p.Get()
	require.NoError(t, err)

	require.EqualValues(t, 1, nativePool.index)
	require.EqualValues(t, 1, nativePool.ref)

	conn.Close()

	require.EqualValues(t, 1, nativePool.index)
	require.EqualValues(t, 0, nativePool.ref)
}

func TestBasicGet2(t *testing.T) {
	opt := DefaultOptions
	opt.Dial = DialTest
	opt.MaxIdle = 1
	opt.MaxActive = 2
	opt.MaxConcurrentStreams = 2
	opt.Reuse = true

	p, nativePool, _, err := newPool(&opt)
	require.NoError(t, err)
	defer p.Close()

	conn1, err := p.Get()
	require.NoError(t, err)
	defer conn1.Close()

	conn2, err := p.Get()
	require.NoError(t, err)
	defer conn2.Close()

	require.EqualValues(t, 2, nativePool.index)
	require.EqualValues(t, 2, nativePool.ref)
	require.EqualValues(t, 1, nativePool.current)

	// create new connections push back to pool
	conn3, err := p.Get()
	require.NoError(t, err)
	defer conn3.Close()

	require.EqualValues(t, 3, nativePool.index)
	require.EqualValues(t, 3, nativePool.ref)
	require.EqualValues(t, 2, nativePool.current)

	conn4, err := p.Get()
	require.NoError(t, err)
	defer conn4.Close()

	// reuse exists connections
	conn5, err := p.Get()
	require.NoError(t, err)
	defer conn5.Close()

	nativeConn := conn5.(*conn)
	require.EqualValues(t, false, nativeConn.once)
}

func TestBasicGet3(t *testing.T) {
	opt := DefaultOptions
	opt.Dial = DialTest
	opt.MaxIdle = 1
	opt.MaxActive = 1
	opt.MaxConcurrentStreams = 1
	opt.Reuse = false

	p, _, _, err := newPool(&opt)
	require.NoError(t, err)
	defer p.Close()

	conn1, err := p.Get()
	require.NoError(t, err)
	defer conn1.Close()

	// create new connections doesn't push back to pool
	conn2, err := p.Get()
	require.NoError(t, err)
	defer conn2.Close()

	nativeConn := conn2.(*conn)
	require.EqualValues(t, true, nativeConn.once)
}

func TestConcurrentGet(t *testing.T) {
	opt := DefaultOptions
	opt.Dial = DialTest
	opt.MaxIdle = 8
	opt.MaxActive = 64
	opt.MaxConcurrentStreams = 2
	opt.Reuse = false

	p, nativePool, _, err := newPool(&opt)
	require.NoError(t, err)
	defer p.Close()

	var wg sync.WaitGroup
	wg.Add(500)

	for i := 0; i < 500; i++ {
		go func(i int) {
			conn, err := p.Get()
			require.NoError(t, err)
			require.EqualValues(t, true, conn != nil)
			conn.Close()
			wg.Done()
			t.Logf("goroutine: %v, index: %v, ref: %v, current: %v", i,
				atomic.LoadUint32(&nativePool.index),
				atomic.LoadInt32(&nativePool.ref),
				atomic.LoadInt32(&nativePool.current))
		}(i)
	}
	wg.Wait()

	require.EqualValues(t, 0, nativePool.ref)
	require.EqualValues(t, opt.MaxIdle, nativePool.current)
	require.EqualValues(t, true, nativePool.conns[0] != nil)
	require.EqualValues(t, true, nativePool.conns[opt.MaxIdle] == nil)
}