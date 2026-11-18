import type { Torrent, TransferStatus } from "@/api/types";

/** A torrent with every field set; override what a test cares about. */
export function torrent(overrides: Partial<Torrent> = {}): Torrent {
  return {
    hash: "a".repeat(40),
    name: "Example",
    state: "downloading",
    complete: false,
    hasMetadata: true,
    progress: 0.5,
    size: 1000,
    totalSize: 1000,
    completed: 500,
    downloaded: 500,
    uploaded: 0,
    sessionDownloaded: 500,
    sessionUploaded: 0,
    multiFile: false,
    trackers: 0,
    downloadRate: 0,
    uploadRate: 0,
    eta: -1,
    ratio: 0,
    seeds: 0,
    peers: 0,
    swarmPeers: 0,
    savePath: "/downloads",
    contentPath: "/downloads/Example",
    contentLayout: "Original",
    rootPath: "/downloads/Example",
    category: "",
    tags: [],
    addedAt: 1_700_000_000,
    completedAt: 0,
    lastActivityAt: 0,
    seedingTime: 0,
    activeTime: 0,
    stopped: false,
    forced: false,
    sequential: false,
    firstLastPiece: false,
    private: false,
    pieceSize: 16384,
    pieces: 10,
    piecesHave: 5,
    magnet: "magnet:?xt=urn:btih:" + "a".repeat(40),
    comment: "",
    createdBy: "",
    createdAt: 0,
    tracker: "",
    ratioLimit: -2,
    seedingTimeLimit: -2,
    inactiveSeedingTimeLimit: -2,
    downloadLimit: 0,
    uploadLimit: 0,
    queuePosition: 0,
    ...overrides,
  };
}

export function transfer(overrides: Partial<TransferStatus> = {}): TransferStatus {
  return {
    downloadRate: 0,
    uploadRate: 0,
    sessionDownloaded: 0,
    sessionUploaded: 0,
    downloadLimit: 0,
    uploadLimit: 0,
    altSpeedEnabled: false,
    port: 6881,
    dhtNodes: 0,
    peers: 0,
    freeSpace: 1 << 30,
    torrents: 0,
    connection: "connected",
    ...overrides,
  };
}

let n = 0;
/** A distinct infohash. */
export function hash(): string {
  n++;
  return n.toString(16).padStart(40, "0");
}
