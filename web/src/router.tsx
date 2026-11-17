import type { QueryClient } from "@tanstack/react-query";
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
} from "@tanstack/react-router";
import { LoginPage } from "@/features/auth/login-page";
import { AppShell } from "@/features/shell/app-shell";
import { NotFound, RouteError } from "@/features/shell/route-states";
import { parseListSearch } from "@/features/torrents/search";
import { TorrentsPage } from "@/features/torrents/torrents-page";
import { meQuery } from "@/lib/queries";

export interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFound,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "login",
  validateSearch: (search: Record<string, unknown>): { redirect?: string } =>
    typeof search.redirect === "string" ? { redirect: search.redirect } : {},
  component: LoginPage,
});

// Everything else needs a signed-in user.
const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context, location }) => {
    const me = await context.queryClient.ensureQueryData(meQuery);
    if (!me) {
      throw redirect({ to: "/login", search: { redirect: location.href } });
    }
    return { me };
  },
  component: AppShell,
  errorComponent: RouteError,
});

// The torrent list stays mounted while a torrent's details are open below it.
const listRoute = createRoute({
  getParentRoute: () => appRoute,
  id: "list",
  validateSearch: parseListSearch,
  component: TorrentsPage,
});

const listIndexRoute = createRoute({
  getParentRoute: () => listRoute,
  path: "/",
});

const detailRoute = createRoute({
  getParentRoute: () => listRoute,
  path: "torrents/$hash",
  component: lazyRouteComponent(() => import("@/features/detail/torrent-detail"), "TorrentDetail"),
});

const categoriesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "categories",
  component: lazyRouteComponent(
    () => import("@/features/categories/categories-page"),
    "CategoriesPage",
  ),
});

const transferRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "transfer",
  component: lazyRouteComponent(() => import("@/features/transfer/transfer-page"), "TransferPage"),
});

const settingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "settings",
  component: lazyRouteComponent(() => import("@/features/settings/settings-page"), "SettingsPage"),
});

const apiKeysRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "api-keys",
  component: lazyRouteComponent(() => import("@/features/admin/api-keys-page"), "APIKeysPage"),
});

const sessionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "sessions",
  component: lazyRouteComponent(() => import("@/features/admin/sessions-page"), "SessionsPage"),
});

// Admin-only pages.
const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "admin",
  beforeLoad: ({ context }) => {
    if (!context.me.isAdmin) {
      throw redirect({ to: "/" });
    }
  },
});

const usersRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "users",
  component: lazyRouteComponent(() => import("@/features/admin/users-page"), "UsersPage"),
});

const auditRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "audit",
  component: lazyRouteComponent(() => import("@/features/admin/audit-page"), "AuditPage"),
});

const logRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "log",
  component: lazyRouteComponent(() => import("@/features/admin/log-page"), "LogPage"),
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([
    listRoute.addChildren([listIndexRoute, detailRoute]),
    categoriesRoute,
    transferRoute,
    settingsRoute,
    apiKeysRoute,
    sessionsRoute,
    adminRoute.addChildren([usersRoute, auditRoute, logRoute]),
  ]),
]);

export function createAppRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    scrollRestoration: true,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
