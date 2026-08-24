package nya

// Legacy zstd sequence code tables.
//
// NYA archives with a minor version of 0 were written by an encoder whose
// Literals_Length and Match_Length tables did not match RFC 8878 Tables 5 and
// 6: the literal length baselines were missing the entry for code 19 (22) and
// the match length baselines were missing the entry for code 35 (41), so every
// later code was shifted down by one slot. Encoder and decoder shared the
// tables, so those archives round-tripped within the old implementation while
// producing frames no conformant zstd decoder could read.
//
// The tables below reproduce that old behaviour verbatim so existing archives
// can still be extracted. Anything written by this package uses the conformant
// tables in zstd_decompress.go and records minor version 1.

var legacyLitLenBaseline = [36]int{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	16, 18, 20, 24, 28, 32, 40, 48, 64, 128, 256, 512, 1024, 2048, 4096, 8192,
	16384, 32768, 65536, 131072,
}

var legacyLitLenExtraBits = [36]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 2, 2, 3, 3, 4, 6, 7, 8, 9, 10, 11, 12, 13,
	14, 15, 16, 17,
}

var legacyMatchLenBaseline = [53]int{
	3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18,
	19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34,
	35, 37, 39, 43, 47, 51, 59, 67, 83, 99, 131, 259, 515, 1027, 2051, 4099,
	8195, 16387, 32771, 65539, 131075,
}

var legacyMatchLenExtraBits = [53]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 2, 2, 2, 3, 3, 4, 4, 5, 7, 8, 9, 10, 11,
	12, 13, 14, 15, 16,
}

var legacySeqCodes = seqCodes{
	llBaseline:  legacyLitLenBaseline[:],
	llExtraBits: legacyLitLenExtraBits[:],
	mlBaseline:  legacyMatchLenBaseline[:],
	mlExtraBits: legacyMatchLenExtraBits[:],
}
