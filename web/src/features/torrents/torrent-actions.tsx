import { useQuery } from "@tanstack/react-query";
import {
  CopyIcon,
  FastForwardIcon,
  FolderIcon,
  MegaphoneIcon,
  PauseIcon,
  PlayIcon,
  PlusIcon,
  RefreshCwIcon,
  TagIcon,
  Trash2Icon,
} from "lucide-react";
import type { Torrent } from "@/api/types";
import {
  ContextMenuCheckboxItem,
  ContextMenuGroup,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuRadioGroup,
  ContextMenuRadioItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenuCheckboxItem,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
} from "@/components/ui/dropdown-menu";
import { copyText } from "@/lib/clipboard";
import { useTorrentAction } from "@/lib/mutations";
import { categoriesQuery, tagsQuery } from "@/lib/queries";

const dropdownParts = {
  Item: DropdownMenuItem,
  Group: DropdownMenuGroup,
  Label: DropdownMenuLabel,
  Separator: DropdownMenuSeparator,
  Sub: DropdownMenuSub,
  SubTrigger: DropdownMenuSubTrigger,
  SubContent: DropdownMenuSubContent,
  CheckboxItem: DropdownMenuCheckboxItem,
  RadioGroup: DropdownMenuRadioGroup,
  RadioItem: DropdownMenuRadioItem,
};

const contextParts = {
  Item: ContextMenuItem,
  Group: ContextMenuGroup,
  Label: ContextMenuLabel,
  Separator: ContextMenuSeparator,
  Sub: ContextMenuSub,
  SubTrigger: ContextMenuSubTrigger,
  SubContent: ContextMenuSubContent,
  CheckboxItem: ContextMenuCheckboxItem,
  RadioGroup: ContextMenuRadioGroup,
  RadioItem: ContextMenuRadioItem,
} as unknown as typeof dropdownParts;

/** Whether toggling should start (some are stopped) or stop the torrents. */
export function shouldStart(torrents: readonly Pick<Torrent, "stopped">[]): boolean {
  return torrents.some((t) => t.stopped);
}

const noCategory = "\u0000none";

/**
 * Every action on the selected torrents, as menu items. The same items fill
 * the toolbar's menu and the list's right-click menu.
 */
export function TorrentActionItems({
  torrents,
  kind,
  onRemove,
  onNewTag,
  compact,
}: {
  torrents: Torrent[];
  kind: "dropdown" | "context";
  onRemove: () => void;
  onNewTag: () => void;
  /** Leave out start and stop, which the toolbar shows as buttons. */
  compact?: boolean;
}) {
  const P = kind === "dropdown" ? dropdownParts : contextParts;
  const action = useTorrentAction();
  const { data: categories = [] } = useQuery(categoriesQuery);
  const { data: tags = [] } = useQuery(tagsQuery);
  const hashes = torrents.map((t) => t.hash);
  const run = (name: Parameters<typeof action.mutate>[0]["action"], extra = {}) =>
    action.mutate({ action: name, hashes, ...extra });

  const sharedCategory = torrents.every((t) => t.category === torrents[0]?.category)
    ? (torrents[0]?.category ?? "")
    : undefined;
  const tagState = (tag: string) => {
    const n = torrents.filter((t) => (t.tags ?? []).includes(tag)).length;
    return n === 0 ? false : n === torrents.length ? true : "mixed";
  };

  return (
    <>
      {!compact && (
        <P.Group>
          <P.Item onClick={() => run("start")}>
            <PlayIcon />
            Start
          </P.Item>
          <P.Item onClick={() => run("stop")}>
            <PauseIcon />
            Stop
          </P.Item>
        </P.Group>
      )}
      <P.Group>
        <P.Item onClick={() => run("forceStart")}>
          <FastForwardIcon />
          Force start
        </P.Item>
        <P.Item onClick={() => run("recheck")}>
          <RefreshCwIcon />
          Recheck
        </P.Item>
        <P.Item onClick={() => run("reannounce")}>
          <MegaphoneIcon />
          Reannounce
        </P.Item>
      </P.Group>
      <P.Separator />
      <P.Sub>
        <P.SubTrigger>
          <FolderIcon />
          Category
        </P.SubTrigger>
        <P.SubContent className="max-h-80">
          <P.RadioGroup
            value={sharedCategory === undefined ? "" : sharedCategory || noCategory}
            onValueChange={(v: string) =>
              run("setCategory", { category: v === noCategory ? "" : v })
            }
          >
            <P.RadioItem value={noCategory}>None</P.RadioItem>
            {categories.map((c) => (
              <P.RadioItem key={c.name} value={c.name}>
                {c.name}
              </P.RadioItem>
            ))}
          </P.RadioGroup>
        </P.SubContent>
      </P.Sub>
      <P.Sub>
        <P.SubTrigger>
          <TagIcon />
          Tags
        </P.SubTrigger>
        <P.SubContent className="max-h-80">
          {tags.length === 0 && <P.Label>No tags yet</P.Label>}
          {tags.map((tag) => {
            const state = tagState(tag);
            return (
              <P.CheckboxItem
                key={tag}
                checked={state === true}
                closeOnClick={false}
                onCheckedChange={(checked: boolean) =>
                  run(checked ? "addTags" : "removeTags", { tags: [tag] })
                }
              >
                {tag}
                {state === "mixed" && (
                  <span className="ml-auto text-muted-foreground normal-case">some</span>
                )}
              </P.CheckboxItem>
            );
          })}
          <P.Separator />
          <P.Item onClick={onNewTag}>
            <PlusIcon />
            New tag…
          </P.Item>
        </P.SubContent>
      </P.Sub>
      <P.Separator />
      <P.Item
        onClick={() =>
          void copyText(
            torrents.map((t) => t.magnet).join("\n"),
            torrents.length === 1 ? "Copied the magnet link" : "Copied the magnet links",
          )
        }
      >
        <CopyIcon />
        Copy magnet {torrents.length === 1 ? "link" : "links"}
      </P.Item>
      <P.Item variant="destructive" onClick={onRemove}>
        <Trash2Icon />
        Remove…
      </P.Item>
    </>
  );
}
