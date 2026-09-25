/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package modsource

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"io"
)

// FileHashes identifies a file in both catalogs: Modrinth by SHA1 (or SHA512), CurseForge by its
// fingerprint.
type FileHashes struct {
	SHA1        string `json:"sha1"`
	SHA512      string `json:"sha512"`
	Fingerprint uint32 `json:"fingerprint"`
}

// Hashes reads r once and returns its hashes and size. The CurseForge fingerprint needs the length
// of the whitespace-stripped content before hashing, so that content is buffered (mod files are
// capped at 256 MB by the caller).
func Hashes(r io.Reader) (FileHashes, int64, error) {
	s1, s5 := sha1.New(), sha512.New()
	var stripped []byte
	buf := make([]byte, 64<<10)
	var n int64
	for {
		k, err := r.Read(buf)
		if k > 0 {
			chunk := buf[:k]
			s1.Write(chunk)
			s5.Write(chunk)
			n += int64(k)
			for _, b := range chunk {
				if b != 9 && b != 10 && b != 13 && b != 32 {
					stripped = append(stripped, b)
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return FileHashes{}, n, err
		}
	}
	return FileHashes{
		SHA1:        hex.EncodeToString(s1.Sum(nil)),
		SHA512:      hex.EncodeToString(s5.Sum(nil)),
		Fingerprint: murmur2(stripped, 1),
	}, n, nil
}

// murmur2 is MurmurHash2 (32-bit), which CurseForge uses (seed 1, whitespace stripped) as the file
// fingerprint.
func murmur2(data []byte, seed uint32) uint32 {
	const m = 0x5bd1e995
	h := seed ^ uint32(len(data))
	for len(data) >= 4 {
		k := binary.LittleEndian.Uint32(data)
		k *= m
		k ^= k >> 24
		k *= m
		h *= m
		h ^= k
		data = data[4:]
	}
	switch len(data) {
	case 3:
		h ^= uint32(data[2]) << 16
		fallthrough
	case 2:
		h ^= uint32(data[1]) << 8
		fallthrough
	case 1:
		h ^= uint32(data[0])
		h *= m
	}
	h ^= h >> 13
	h *= m
	h ^= h >> 15
	return h
}
