// Copyright The OpenTelemetry Authors
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
// See the License for the specific language governing permissions and
// limitations under the License.

package filestorage

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
)

func TestClientOperations(t *testing.T) {
	tempDir := newTempDir(t)
	dbFile := filepath.Join(tempDir, "my_db")

	client, err := newClient(dbFile, time.Second)
	require.NoError(t, err)

	ctx := context.Background()
	testKey := "testKey"
	testValue := []byte("testValue")

	// Make sure nothing is there
	value, err := client.Get(ctx, testKey)
	require.NoError(t, err)
	require.Nil(t, value)

	// Set it
	err = client.Set(ctx, testKey, testValue)
	require.NoError(t, err)

	// Get it back out, make sure it's right
	value, err = client.Get(ctx, testKey)
	require.NoError(t, err)
	require.Equal(t, testValue, value)

	// Delete it
	err = client.Delete(ctx, testKey)
	require.NoError(t, err)

	// Make sure it's gone
	value, err = client.Get(ctx, testKey)
	require.NoError(t, err)
	require.Nil(t, value)
}

func TestClientBatchOperations(t *testing.T) {
	tempDir := newTempDir(t)
	dbFile := filepath.Join(tempDir, "my_db")

	client, err := newClient(dbFile, time.Second)
	require.NoError(t, err)

	ctx := context.Background()
	testEntries := map[string][]byte{
		"testKey1": []byte("testValue1"),
		"testKey2": []byte("testValue2"),
	}
	testKeys := []string{"testKey1", "testKey2"}

	// Make sure nothing is there
	value, err := client.GetBatch(ctx, testKeys)
	require.NoError(t, err)
	require.Equal(t, value, [][]byte{nil, nil})

	// Set it
	err = client.SetBatch(ctx, testEntries)
	require.NoError(t, err)

	// Get it back out, make sure it's right
	values, err := client.GetBatch(ctx, testKeys)
	require.NoError(t, err)
	require.Len(t, values, 2)
	for i, key := range testKeys {
		require.Equal(t, testEntries[key], values[i])
	}

	// Update it (the second entry should get removed)
	testEntriesUpdate := map[string][]byte{
		"testKey1": []byte("testValue1"),
		"testKey2": nil,
	}
	err = client.SetBatch(ctx, testEntriesUpdate)
	require.NoError(t, err)

	// Get it back out, make sure it's right
	valuesUpdate, err := client.GetBatch(ctx, testKeys)
	require.NoError(t, err)
	require.Len(t, values, 2)
	for i, key := range testKeys {
		require.Equal(t, testEntriesUpdate[key], valuesUpdate[i])
	}

	// Delete it all
	err = client.DeleteBatch(ctx, testKeys)
	require.NoError(t, err)

	// Make sure it's gone
	value, err = client.GetBatch(ctx, testKeys)
	require.NoError(t, err)
	require.Equal(t, value, [][]byte{nil, nil})
}

func TestNewClientTransactionErrors(t *testing.T) {
	timeout := 100 * time.Millisecond

	testKey := "testKey"
	testValue := []byte("testValue")

	testCases := []struct {
		name     string
		setup    func(*bbolt.Tx) error
		validate func(*testing.T, *fileStorageClient)
	}{
		{
			name: "get",
			setup: func(tx *bbolt.Tx) error {
				return tx.DeleteBucket(defaultBucket)
			},
			validate: func(t *testing.T, c *fileStorageClient) {
				value, err := c.Get(context.Background(), testKey)
				require.Error(t, err)
				require.Equal(t, "storage not initialized", err.Error())
				require.Nil(t, value)
			},
		},
		{
			name: "set",
			setup: func(tx *bbolt.Tx) error {
				return tx.DeleteBucket(defaultBucket)
			},
			validate: func(t *testing.T, c *fileStorageClient) {
				err := c.Set(context.Background(), testKey, testValue)
				require.Error(t, err)
				require.Equal(t, "storage not initialized", err.Error())
			},
		},
		{
			name: "delete",
			setup: func(tx *bbolt.Tx) error {
				return tx.DeleteBucket(defaultBucket)
			},
			validate: func(t *testing.T, c *fileStorageClient) {
				err := c.Delete(context.Background(), testKey)
				require.Error(t, err)
				require.Equal(t, "storage not initialized", err.Error())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			tempDir := newTempDir(t)
			dbFile := filepath.Join(tempDir, "my_db")

			client, err := newClient(dbFile, timeout)
			require.NoError(t, err)

			// Create a problem
			client.db.Update(tc.setup)

			// Validate expected behavior
			tc.validate(t, client)
		})
	}
}

func TestNewClientErrorsOnInvalidBucket(t *testing.T) {
	temp := defaultBucket
	defaultBucket = nil

	tempDir := newTempDir(t)
	dbFile := filepath.Join(tempDir, "my_db")

	client, err := newClient(dbFile, time.Second)
	require.Error(t, err)
	require.Nil(t, client)

	defaultBucket = temp
}

func BenchmarkClientGet(b *testing.B) {
	tempDir := newTempDir(b)
	dbFile := filepath.Join(tempDir, "my_db")

	client, err := newClient(dbFile, time.Second)
	require.NoError(b, err)

	ctx := context.Background()
	testKey := "testKey"

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		client.Get(ctx, testKey)
	}
}

func BenchmarkClientGet100(b *testing.B) {
	tempDir := newTempDir(b)
	dbFile := filepath.Join(tempDir, "my_db")

	client, err := newClient(dbFile, time.Second)
	require.NoError(b, err)

	ctx := context.Background()

	testKeys := make([]string, 100)
	for i := 0; i < 100; i++ {
		testKeys[i] = "testKey"
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		client.GetBatch(ctx, testKeys)
	}
}

func BenchmarkClientSet(b *testing.B) {
	tempDir := newTempDir(b)
	dbFile := filepath.Join(tempDir, "my_db")

	client, err := newClient(dbFile, time.Second)
	require.NoError(b, err)

	ctx := context.Background()
	testKey := "testKey"
	testValue := []byte("testValue")

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		client.Set(ctx, testKey, testValue)
	}
}

func BenchmarkClientSet100(b *testing.B) {
	tempDir := newTempDir(b)
	dbFile := filepath.Join(tempDir, "my_db")

	client, err := newClient(dbFile, time.Second)
	require.NoError(b, err)

	ctx := context.Background()

	testEntries := make(map[string][]byte)
	for i := 0; i < 100; i++ {
		testEntries[fmt.Sprintf("testKey-%d", i)] = []byte("testValue")
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		client.SetBatch(ctx, testEntries)
	}
}

func BenchmarkClientDelete(b *testing.B) {
	tempDir := newTempDir(b)
	dbFile := filepath.Join(tempDir, "my_db")

	client, err := newClient(dbFile, time.Second)
	require.NoError(b, err)

	ctx := context.Background()
	testKey := "testKey"

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		client.Delete(ctx, testKey)
	}
}

func newTempDir(tb testing.TB) string {
	tempDir, err := ioutil.TempDir("", "")
	require.NoError(tb, err)
	tb.Cleanup(func() { os.RemoveAll(tempDir) })
	return tempDir
}
