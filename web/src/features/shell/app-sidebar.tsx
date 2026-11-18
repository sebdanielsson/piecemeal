import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useMatchRoute, useNavigate, useSearch } from "@tanstack/react-router";
import {
  ActivityIcon,
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronDownIcon,
  CircleCheckIcon,
  CircleDashedIcon,
  ClipboardListIcon,
  FolderIcon,
  GaugeIcon,
  GlobeIcon,
  HourglassIcon,
  KeyRoundIcon,
  KeyboardIcon,
  ListIcon,
  LogOutIcon,
  MonitorIcon,
  MoonIcon,
  PauseIcon,
  PlusIcon,
  RefreshCwIcon,
  ScrollTextIcon,
  Settings2Icon,
  SettingsIcon,
  SunIcon,
  TagIcon,
  TagsIcon,
  TriangleAlertIcon,
  UserIcon,
  UsersIcon,
  type LucideIcon,
  RssIcon,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { api } from "@/api/client";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { unwrap } from "@/lib/api";
import { countTorrents, stateFilterLabels, stateFilters, type StateFilter } from "@/lib/filters";
import type { TorrentMap } from "@/lib/live/reducer";
import { categoriesQuery, forgetSession, meQuery, tagsQuery, torrentsQuery } from "@/lib/queries";
import { readStorage, writeStorage } from "@/lib/storage";
import { useTheme, type ThemePreference } from "@/lib/theme";
import { openAddDialog } from "../torrents/add-store";
import { parseListSearch, type ListSearch } from "../torrents/search";
import { Logo } from "./logo";

const stateIcons: Record<StateFilter, LucideIcon> = {
  all: ListIcon,
  downloading: ArrowDownIcon,
  seeding: ArrowUpIcon,
  completed: CircleCheckIcon,
  stopped: PauseIcon,
  active: ActivityIcon,
  inactive: CircleDashedIcon,
  stalled: HourglassIcon,
  checking: RefreshCwIcon,
  errored: TriangleAlertIcon,
};

export function AppSidebar({ onShowShortcuts }: { onShowShortcuts: () => void }) {
  const { data: me } = useQuery(meQuery);
  const { data: counts } = useQuery({ ...torrentsQuery, select: selectCounts });
  const { data: categories = [] } = useQuery(categoriesQuery);
  const { data: tags = [] } = useQuery(tagsQuery);
  const matchRoute = useMatchRoute();
  const onList = Boolean(matchRoute({ to: "/" }) || matchRoute({ to: "/torrents/$hash" }));
  const current = parseListSearch(useSearch({ strict: false }) as Record<string, unknown>);
  const base: ListSearch = onList ? current : {};
  const { isMobile, setOpenMobile } = useSidebar();
  const close = () => isMobile && setOpenMobile(false);

  const trackers = [...(counts?.trackers.keys() ?? [])].toSorted((a, b) =>
    a === "" ? 1 : b === "" ? -1 : a.localeCompare(b),
  );

  return (
    <Sidebar>
      <SidebarHeader className="flex-row items-center gap-2.5 px-4 pt-4">
        <Link to="/" className="flex min-w-0 flex-1 items-center gap-2.5" onClick={close}>
          <Logo className="size-7" />
          <span className="truncate font-heading text-xl">piecemeal</span>
        </Link>
        <Tooltip>
          <TooltipTrigger
            className="flex size-8 items-center justify-center bg-primary text-primary-foreground hover:bg-primary/80 focus-visible:ring-2 focus-visible:ring-ring/30 focus-visible:outline-none"
            onClick={() => {
              close();
              openAddDialog();
            }}
            aria-label="Add torrents"
          >
            <PlusIcon className="size-4" />
          </TooltipTrigger>
          <TooltipContent>Add torrents (A)</TooltipContent>
        </Tooltip>
      </SidebarHeader>
      <SidebarContent className="gap-0">
        <SidebarGroup>
          <SidebarMenu>
            <NavItem to="/" icon={ListIcon} label="Torrents" active={onList} onClick={close} />
            <NavItem to="/transfer" icon={GaugeIcon} label="Transfer" onClick={close} />
            <NavItem to="/categories" icon={FolderIcon} label="Categories & tags" onClick={close} />
            <NavItem to="/rss" icon={RssIcon} label="RSS" onClick={close} />
            <NavItem to="/settings" icon={SettingsIcon} label="Settings" onClick={close} />
          </SidebarMenu>
        </SidebarGroup>

        <FilterGroup id="status" label="Status">
          {stateFilters.map((f) => (
            <FilterItem
              key={f}
              icon={stateIcons[f]}
              label={stateFilterLabels[f]}
              count={counts?.states[f]}
              active={onList && (current.filter ?? "all") === f}
              search={{ ...base, filter: f === "all" ? undefined : f }}
              onClick={close}
            />
          ))}
        </FilterGroup>

        <FilterGroup
          id="categories"
          label="Categories"
          action={
            <SidebarGroupAction
              render={<Link to="/categories" aria-label="Manage categories" onClick={close} />}
            >
              <Settings2Icon />
            </SidebarGroupAction>
          }
        >
          <FilterItem
            icon={FolderIcon}
            label="All"
            count={counts?.total}
            active={onList && current.category === undefined}
            search={{ ...base, category: undefined }}
            onClick={close}
          />
          <FilterItem
            icon={FolderIcon}
            label="Uncategorized"
            muted
            count={counts?.categories.get("") ?? 0}
            active={onList && current.category === ""}
            search={{ ...base, category: "" }}
            onClick={close}
          />
          {categories.map((c) => (
            <FilterItem
              key={c.name}
              icon={FolderIcon}
              label={c.name}
              count={counts?.categories.get(c.name) ?? 0}
              active={onList && current.category === c.name}
              search={{ ...base, category: c.name }}
              onClick={close}
            />
          ))}
        </FilterGroup>

        <FilterGroup id="tags" label="Tags">
          <FilterItem
            icon={TagsIcon}
            label="All"
            count={counts?.total}
            active={onList && current.tag === undefined}
            search={{ ...base, tag: undefined }}
            onClick={close}
          />
          <FilterItem
            icon={TagIcon}
            label="Untagged"
            muted
            count={counts?.tags.get("") ?? 0}
            active={onList && current.tag === ""}
            search={{ ...base, tag: "" }}
            onClick={close}
          />
          {tags.map((t) => (
            <FilterItem
              key={t}
              icon={TagIcon}
              label={t}
              count={counts?.tags.get(t) ?? 0}
              active={onList && current.tag === t}
              search={{ ...base, tag: t }}
              onClick={close}
            />
          ))}
        </FilterGroup>

        <FilterGroup id="trackers" label="Trackers">
          <FilterItem
            icon={GlobeIcon}
            label="All"
            count={counts?.total}
            active={onList && current.tracker === undefined}
            search={{ ...base, tracker: undefined }}
            onClick={close}
          />
          {trackers.map((host) => (
            <FilterItem
              key={host || "(none)"}
              icon={GlobeIcon}
              label={host || "Trackerless"}
              muted={host === ""}
              count={counts?.trackers.get(host) ?? 0}
              active={onList && current.tracker === host}
              search={{ ...base, tracker: host }}
              onClick={close}
            />
          ))}
        </FilterGroup>

        <SidebarGroup>
          <SidebarGroupLabel>Access</SidebarGroupLabel>
          <SidebarMenu>
            <NavItem to="/api-keys" icon={KeyRoundIcon} label="API keys" onClick={close} />
            <NavItem to="/sessions" icon={MonitorIcon} label="Sessions" onClick={close} />
            {me?.isAdmin && (
              <>
                <NavItem to="/admin/users" icon={UsersIcon} label="Users" onClick={close} />
                <NavItem
                  to="/admin/audit"
                  icon={ClipboardListIcon}
                  label="Audit log"
                  onClick={close}
                />
                <NavItem to="/admin/log" icon={ScrollTextIcon} label="Log" onClick={close} />
              </>
            )}
          </SidebarMenu>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <UserMenu
          name={me?.name || me?.username || "Signed in"}
          onShowShortcuts={onShowShortcuts}
        />
      </SidebarFooter>
    </Sidebar>
  );
}

const selectCounts = (m: TorrentMap) => countTorrents(m.values());

function NavItem({
  to,
  icon: Icon,
  label,
  active,
  onClick,
}: {
  to:
    | "/"
    | "/transfer"
    | "/categories"
    | "/rss"
    | "/settings"
    | "/api-keys"
    | "/sessions"
    | "/admin/users"
    | "/admin/audit"
    | "/admin/log";
  icon: LucideIcon;
  label: string;
  active?: boolean;
  onClick?: () => void;
}) {
  const matchRoute = useMatchRoute();
  const isActive = active ?? Boolean(matchRoute({ to, fuzzy: true }));
  return (
    <SidebarMenuItem>
      <SidebarMenuButton size="sm" isActive={isActive} render={<Link to={to} onClick={onClick} />}>
        <Icon />
        <span>{label}</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}

function FilterGroup({
  id,
  label,
  action,
  children,
}: {
  id: string;
  label: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  const storageKey = `piecemeal.sidebar.${id}`;
  const [open, setOpen] = useState(() => readStorage(storageKey, true));
  const toggle = () => {
    setOpen(!open);
    writeStorage(storageKey, !open);
  };
  return (
    <SidebarGroup className="py-1">
      <SidebarGroupLabel
        render={
          <button type="button" onClick={toggle} aria-expanded={open} className="w-full gap-1.5" />
        }
      >
        <ChevronDownIcon className={open ? "" : "-rotate-90"} />
        {label}
      </SidebarGroupLabel>
      {action}
      {open && (
        <SidebarGroupContent>
          <SidebarMenu>{children}</SidebarMenu>
        </SidebarGroupContent>
      )}
    </SidebarGroup>
  );
}

function FilterItem({
  icon: Icon,
  label,
  count,
  active,
  muted,
  search,
  onClick,
}: {
  icon: LucideIcon;
  label: string;
  count: number | undefined;
  active: boolean;
  muted?: boolean;
  search: ListSearch;
  onClick?: () => void;
}) {
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        size="sm"
        isActive={active}
        render={<Link to="/" search={search} onClick={onClick} />}
        className={muted ? "text-sidebar-foreground/70 italic" : undefined}
      >
        <Icon />
        <span>{label}</span>
      </SidebarMenuButton>
      {count !== undefined && (
        <SidebarMenuBadge className="tabular">{count.toLocaleString()}</SidebarMenuBadge>
      )}
    </SidebarMenuItem>
  );
}

const themeOptions: { value: ThemePreference; label: string; icon: LucideIcon }[] = [
  { value: "system", label: "System", icon: MonitorIcon },
  { value: "light", label: "Light", icon: SunIcon },
  { value: "dark", label: "Dark", icon: MoonIcon },
];

function UserMenu({ name, onShowShortcuts }: { name: string; onShowShortcuts: () => void }) {
  const { preference, setPreference } = useTheme();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const signOut = useMutation({
    mutationFn: () => unwrap(api.POST("/auth/logout")),
    onSettled: async () => {
      await navigate({ to: "/login", search: {} });
      forgetSession(queryClient);
    },
  });
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger render={<SidebarMenuButton className="h-10" />}>
            <UserIcon />
            <span className="flex-1 truncate">{name}</span>
            <ChevronDownIcon className="ml-auto" />
          </DropdownMenuTrigger>
          <DropdownMenuContent side="top" className="w-(--anchor-width) min-w-56">
            <DropdownMenuGroup>
              <DropdownMenuLabel>Theme</DropdownMenuLabel>
              <DropdownMenuRadioGroup
                value={preference}
                onValueChange={(v) => setPreference(v as ThemePreference)}
              >
                {themeOptions.map((o) => (
                  <DropdownMenuRadioItem key={o.value} value={o.value}>
                    <o.icon />
                    {o.label}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={onShowShortcuts}>
              <KeyboardIcon />
              Keyboard shortcuts
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => signOut.mutate()} disabled={signOut.isPending}>
              <LogOutIcon />
              Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
