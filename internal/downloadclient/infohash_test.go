package downloadclient

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // matches the v1 infohash definition under test.
	"encoding/hex"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

// minimalTorrent builds a tiny but valid single-file .torrent and returns the
// full bytes alongside the raw info-dict bytes (whose SHA-1 is the infohash).
func minimalTorrent() (torrent, info []byte) {
	info = []byte("d6:lengthi12e4:name8:test.txt12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaae")
	var b bytes.Buffer
	b.WriteString("d8:announce13:udp://x:1337/4:info")
	b.Write(info)
	b.WriteByte('e')
	return b.Bytes(), info
}

func TestTorrentInfoHash(t *testing.T) {
	torrent, info := minimalTorrent()
	sum := sha1.Sum(info) //nolint:gosec // infohash definition
	want := hex.EncodeToString(sum[:])

	got, err := torrentInfoHash(torrent)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("infohash = %s want %s", got, want)
	}
}

func TestTorrentInfoHashRejectsGarbage(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("not bencode"), []byte("d3:foo3:bare")} {
		if _, err := torrentInfoHash(in); err == nil {
			t.Fatalf("expected error for %q", in)
		}
	}
}

// A .torrent-file grab (Content set, no magnet URL) must return the computed v1
// infohash so the import pipeline can attribute it — previously it returned "".
func TestQBittorrentAddTorrentFileReturnsInfoHash(t *testing.T) {
	srv := newQBitServer(t)
	defer srv.Close()
	c := newQBittorrent("2", srv.URL, "admin", "pw", "movies", srv.Client())

	torrent, info := minimalTorrent()
	sum := sha1.Sum(info) //nolint:gosec // infohash definition
	want := hex.EncodeToString(sum[:])

	id, err := c.Add(context.Background(), provider.DownloadRequest{
		Content: torrent, Title: "Some.Release", Protocol: provider.ProtocolTorrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != want {
		t.Fatalf("add id = %q want infohash %s", id, want)
	}
}
