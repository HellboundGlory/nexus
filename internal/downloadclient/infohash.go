package downloadclient

import (
	"crypto/sha1" //nolint:gosec // BitTorrent v1 infohash is defined as SHA-1(info); not a security use.
	"encoding/hex"
	"errors"
	"strconv"
)

// errBencode marks a .torrent body that isn't well-formed bencode, and errNoInfo
// a top-level dict with no "info" key.
var (
	errBencode = errors.New("downloadclient: malformed bencode")
	errNoInfo  = errors.New("downloadclient: torrent has no info dict")
)

// torrentInfoHash computes the BitTorrent v1 infohash of a .torrent file: the
// lowercase hex SHA-1 of the raw bencoded "info" dictionary. This equals the hash
// qBittorrent reports for the added torrent, so it lets Nexus attribute a
// .torrent-file grab (which qBit's add endpoint returns no id for) to its
// download-queue row. v2-only torrents (SHA-256 identity) are not covered.
func torrentInfoHash(content []byte) (string, error) {
	if len(content) == 0 || content[0] != 'd' {
		return "", errBencode
	}
	i := 1
	for i < len(content) && content[i] != 'e' {
		key, keyEnd, err := bencodeString(content, i)
		if err != nil {
			return "", err
		}
		valEnd, err := scanValue(content, keyEnd)
		if err != nil {
			return "", err
		}
		if key == "info" {
			sum := sha1.Sum(content[keyEnd:valEnd]) //nolint:gosec // see file header
			return hex.EncodeToString(sum[:]), nil
		}
		i = valEnd
	}
	return "", errNoInfo
}

// bencodeString reads a bencoded string (<len>:<bytes>) at i, returning its value
// and the index just past it.
func bencodeString(b []byte, i int) (string, int, error) {
	j := i
	for j < len(b) && b[j] != ':' {
		if b[j] < '0' || b[j] > '9' {
			return "", 0, errBencode
		}
		j++
	}
	if j >= len(b) {
		return "", 0, errBencode
	}
	n, err := strconv.Atoi(string(b[i:j]))
	if err != nil || n < 0 {
		return "", 0, errBencode
	}
	start := j + 1
	end := start + n
	if end > len(b) {
		return "", 0, errBencode
	}
	return string(b[start:end]), end, nil
}

// scanValue returns the index just past the bencoded value starting at i
// (integer, string, list, or dict), without allocating the value itself.
func scanValue(b []byte, i int) (int, error) {
	if i >= len(b) {
		return 0, errBencode
	}
	switch c := b[i]; {
	case c == 'i': // i<digits>e
		j := i + 1
		for j < len(b) && b[j] != 'e' {
			j++
		}
		if j >= len(b) {
			return 0, errBencode
		}
		return j + 1, nil
	case c == 'l' || c == 'd': // list / dict: nested values until 'e'
		j := i + 1
		for j < len(b) && b[j] != 'e' {
			var err error
			if j, err = scanValue(b, j); err != nil {
				return 0, err
			}
		}
		if j >= len(b) {
			return 0, errBencode
		}
		return j + 1, nil
	case c >= '0' && c <= '9': // string
		_, end, err := bencodeString(b, i)
		return end, err
	default:
		return 0, errBencode
	}
}
