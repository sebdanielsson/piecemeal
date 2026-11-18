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
        key: "addStopped",
        label: "Add torrents stopped",
        description: "New torrents wait until you start them.",
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
  {
    title: "Seeding goals",
    description: "Defaults for torrents that follow the global setting.",
    fields: [
      { key: "ratioLimit", label: "Stop at ratio", kind: "ratio" },
      { key: "seedingTimeLimit", label: "Stop after seeding for", kind: "minutes" },
      { key: "inactiveSeedingTimeLimit", label: "Stop when inactive for", kind: "minutes" },
      {
        key: "seedGoalAction",
        label: "When a goal is reached",
        kind: "select",
        options: [
          { value: "stop", label: "Stop the torrent" },
          { value: "remove", label: "Remove the torrent" },
          { value: "removeWithData", label: "Remove the torrent and its files" },
        ],
      },
    ],
  },
];
