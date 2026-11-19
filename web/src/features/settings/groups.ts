import { seedGoalActions, seedGoalModes } from "@/components/form/seed-goal-options";
import type { Settings } from "@/api/types";
import type { FieldGroupDef } from "./settings-form";

export const speedGroup: FieldGroupDef = {
  title: "Speed limits",
  description: "Across all torrents. Empty or 0 is unlimited.",
  fields: [
    { key: "downloadLimit", label: "Download limit", kind: "limit" },
    { key: "uploadLimit", label: "Upload limit", kind: "limit" },
  ],
};

export const altSpeedGroup: FieldGroupDef = {
  title: "Alternative speed limits",
  description: "Used instead of the limits above while they are on, for example at peak hours.",
  fields: [
    { key: "altSpeedEnabled", label: "Use the alternative limits", kind: "bool" },
    { key: "altDownloadLimit", label: "Alternative download limit", kind: "limit" },
    { key: "altUploadLimit", label: "Alternative upload limit", kind: "limit" },
  ],
};

export const scheduleGroup: FieldGroupDef = {
  title: "Alternative speed schedule",
  description:
    "Turns the alternative limits on and off at set times, in piecemeal's local time. A window that ends before it starts runs past midnight.",
  fields: [
    { key: "altSpeedSchedule", label: "Follow the schedule", kind: "bool" },
    { key: "altSpeedScheduleFrom", label: "From", kind: "time" },
    { key: "altSpeedScheduleTo", label: "To", kind: "time" },
    {
      key: "altSpeedScheduleDays",
      label: "On",
      kind: "select",
      options: [
        { value: "everyDay", label: "Every day" },
        { value: "weekdays", label: "Weekdays" },
        { value: "weekends", label: "Weekends" },
        { value: "monday", label: "Monday" },
        { value: "tuesday", label: "Tuesday" },
        { value: "wednesday", label: "Wednesday" },
        { value: "thursday", label: "Thursday" },
        { value: "friday", label: "Friday" },
        { value: "saturday", label: "Saturday" },
        { value: "sunday", label: "Sunday" },
      ],
    },
  ],
};

export const rssGroup: FieldGroupDef = {
  title: "RSS",
  description: "Feeds and download rules are on the RSS page.",
  fields: [
    {
      key: "rssEnabled",
      label: "Fetch feeds",
      description: "On their schedule; refreshing by hand works either way.",
      kind: "bool",
    },
    {
      key: "rssRefreshInterval",
      label: "Refresh every (minutes)",
      description: "For feeds without their own interval.",
      kind: "int",
      min: 1,
    },
    {
      key: "rssAutoDownload",
      label: "Run download rules",
      description: "Add the torrents of new articles that rules match.",
      kind: "bool",
    },
    { key: "rssMaxArticles", label: "Articles kept per feed", kind: "int", min: 0 },
    { key: "rssFetchDelay", label: "Seconds between requests to a host", kind: "int", min: 0 },
    {
      key: "rssDownloadRepacks",
      label: "Take REPACK and PROPER releases",
      description: "Of episodes the smart filter already took.",
      kind: "bool",
    },
    {
      key: "rssSmartEpisodeFilters",
      label: "Smart episode filters",
      description:
        "Regular expressions, one per line, that find the episode in a title: two groups for season and episode, or one for a date.",
      kind: "lines",
    },
  ],
};

const proxyOn = (s: Settings) => s.proxyType !== "none";
const btProxied = (s: Settings) => proxyOn(s) && s.proxyBitTorrent;

export const proxyGroup: FieldGroupDef = {
  title: "Proxy",
  description:
    "Sends traffic through a SOCKS5 or HTTP proxy, for when piecemeal can't run inside a VPN. Nothing set to use the proxy goes around it: what the proxy can't carry is turned off or fails.",
  fields: [
    {
      key: "proxyType",
      label: "Proxy",
      kind: "select",
      options: [
        { value: "none", label: "None" },
        { value: "socks5", label: "SOCKS5" },
        { value: "http", label: "HTTP" },
      ],
    },
    { key: "proxyHost", label: "Host", kind: "text", shownWhen: proxyOn },
    { key: "proxyPort", label: "Port", kind: "int", min: 1, max: 65535, shownWhen: proxyOn },
    {
      key: "proxyUsername",
      label: "Username",
      description: "Empty if the proxy doesn't ask for one.",
      kind: "text",
      shownWhen: proxyOn,
    },
    {
      key: "proxyPassword",
      label: "Password",
      description: "Never shown again once saved. Clearing the username removes it.",
      kind: "password",
      shownWhen: (s) => proxyOn(s) && s.proxyUsername !== "",
    },
    {
      key: "proxyBitTorrent",
      label: "Trackers and web seeds",
      description:
        "UDP trackers need a SOCKS5 proxy that relays UDP; through an HTTP proxy they aren't used.",
      kind: "bool",
      shownWhen: proxyOn,
    },
    {
      key: "proxyPeerConnections",
      label: "Peer connections",
      description:
        "The DHT, µTP and WebRTC peers can't go through a proxy, so they're off while this is on.",
      kind: "bool",
      shownWhen: btProxied,
    },
    {
      key: "proxyHostnameLookup",
      label: "Let the proxy look up hostnames",
      description:
        "Of trackers and web seeds. UDP trackers, torrent files and feeds are looked up by piecemeal.",
      kind: "bool",
      shownWhen: btProxied,
    },
    {
      key: "proxyTorrentFiles",
      label: "Torrent files from URLs",
      description:
        "Added by URL, by the *arrs or by RSS rules. Addresses on PIECEMEAL_FETCH_ALLOWED_NETWORKS, such as Prowlarr's, are reached directly.",
      kind: "bool",
      shownWhen: proxyOn,
    },
    { key: "proxyRSS", label: "RSS feeds", kind: "bool", shownWhen: proxyOn },
  ],
};

export const settingsGroups: FieldGroupDef[] = [
  {
    title: "Downloads",
    fields: [
      {
        key: "defaultSavePath",
        label: "Default save path",
        description: "Where torrents go when neither the request nor its category names a path.",
        kind: "path",
      },
      {
        key: "incompletePathEnabled",
        label: "Keep incomplete torrents apart",
        description:
          "Torrents download into the incomplete folder and move to their save path once complete. A category or a torrent can choose otherwise.",
        kind: "bool",
      },
      {
        key: "incompletePath",
        label: "Incomplete folder",
        kind: "path",
      },
      {
        key: "addStopped",
        label: "Add torrents stopped",
        description: "New torrents wait until you start them.",
        kind: "bool",
      },
      {
        key: "maxActiveCheckingTorrents",
        label: "Torrents checked at once",
        description:
          "Checking data reads all of it. Others wait, queued for checking; on hard drives, one at a time keeps the disks from seeking between torrents.",
        kind: "int",
        min: 1,
      },
    ],
  },
  {
    title: "Automatic management",
    description:
      "An automatically managed torrent's save path and incomplete folder follow its category: the category's paths, or a folder named after the category in the default save path. When those paths change, its data moves, or it switches to manual management, as set below.",
    fields: [
      {
        key: "autoManagement",
        label: "Manage new torrents automatically",
        description: "Unless they're added with a save path of their own.",
        kind: "bool",
      },
      {
        key: "relocateOnCategoryChange",
        label: "Move when the category changes",
        description: "Off switches the torrent to manual management instead.",
        kind: "bool",
      },
      {
        key: "relocateOnCategoryPathChange",
        label: "Move when the category's paths change",
        description: "Off switches its torrents to manual management instead.",
        kind: "bool",
      },
      {
        key: "relocateOnDefaultPathChange",
        label: "Move when the default save path or incomplete folder changes",
        description: "Off switches the torrents that use them to manual management instead.",
        kind: "bool",
      },
    ],
  },
  speedGroup,
  altSpeedGroup,
  scheduleGroup,
  {
    title: "Queueing",
    description:
      "Limits how many torrents run at once; the rest wait in the queue. Forced torrents don't count. -1 means no limit.",
    fields: [
      { key: "queueingEnabled", label: "Queue torrents", kind: "bool" },
      { key: "maxActiveDownloads", label: "Active downloads", kind: "int", min: -1 },
      { key: "maxActiveUploads", label: "Active uploads", kind: "int", min: -1 },
      { key: "maxActiveTorrents", label: "Active torrents", kind: "int", min: -1 },
      {
        key: "dontCountSlowTorrents",
        label: "Don't count slow torrents",
        description: "Torrents below the rates below for long enough let another one start.",
        kind: "bool",
      },
      { key: "slowDownloadRate", label: "Slow download rate", kind: "limit" },
      { key: "slowUploadRate", label: "Slow upload rate", kind: "limit" },
      {
        key: "slowTorrentTime",
        label: "Slow for (seconds)",
        kind: "int",
        min: 0,
      },
    ],
  },
  rssGroup,
  {
    title: "IP filtering",
    fields: [
      {
        key: "ipFilterEnabled",
        label: "Use an IP filter",
        description: "Refuse peers in the ranges of a filter file.",
        kind: "bool",
      },
      {
        key: "ipFilterPath",
        label: "Filter file",
        description: "eMule .dat, PeerGuardian .p2p, or one CIDR per line.",
        kind: "path",
      },
      {
        key: "bannedIPs",
        label: "Banned addresses",
        description: "One IP address per line. Always refused, filter or not.",
        kind: "lines",
      },
    ],
  },
  {
    title: "Connection",
    fields: [
      {
        key: "peerPort",
        label: "Peer port",
        description:
          "The port peers connect to. NAT-PMP, when configured, overrides it; 0 picks a random port.",
        kind: "int",
        min: 0,
        max: 65535,
      },
      {
        key: "maxConnectionsPerTorrent",
        label: "Connections per torrent",
        description: "Established peer connections for each torrent.",
        kind: "int",
        min: 1,
      },
      {
        key: "maxHalfOpenConnections",
        label: "Connection attempts",
        description: "Outgoing connection attempts in progress, across all torrents.",
        kind: "int",
        min: 1,
      },
      {
        key: "utp",
        label: "Use µTP",
        description: "Connect over µTP as well as TCP.",
        kind: "bool",
      },
    ],
  },
  {
    title: "BitTorrent",
    fields: [
      {
        key: "dht",
        label: "DHT",
        description: "Find peers through the distributed hash table.",
        kind: "bool",
      },
      {
        key: "pex",
        label: "Peer exchange",
        description: "Swap peer addresses with connected peers.",
        kind: "bool",
      },
      {
        key: "encryption",
        label: "Encryption",
        kind: "select",
        options: [
          { value: "prefer", label: "Prefer encryption" },
          { value: "require", label: "Require encryption" },
          { value: "disable", label: "Disable encryption" },
        ],
      },
    ],
  },
  proxyGroup,
  {
    title: "Seeding goals",
    description: "For torrents that follow the default and whose category doesn't set its own.",
    fields: [
      { key: "ratioLimit", label: "Ratio goal", kind: "ratio" },
      { key: "seedingTimeLimit", label: "Seeding time goal", kind: "minutes" },
      { key: "inactiveSeedingTimeLimit", label: "Inactive time goal", kind: "minutes" },
      { key: "seedGoalMode", label: "Reached when", kind: "select", options: seedGoalModes },
      { key: "seedGoalAction", label: "Then", kind: "select", options: seedGoalActions },
    ],
  },
];
