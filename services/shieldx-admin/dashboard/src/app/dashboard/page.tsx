import { useCallback, useEffect, useState } from "react"

import { AppSidebar } from "@/components/app-sidebar"
import { Button } from "@/components/ui/button"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"

import { AvgLatency } from "../components/AvgLatency"
import { ChartAreaInteractive } from "../components/chart-area-interactive"
import { ChartPieInteractive } from "../components/ChartPieDonutText"
import { ChartLineDefault } from "../components/chartTotalRequest"
import { EventInvestigation, type EventLog } from "../components/EventInvestigation"
import { LatestAlerts, type AlertRecord } from "../components/LatestAlerts"
import {
  fetchEventLogs,
  fetchLatestAlerts,
  fetchServiceHealth,
  type ServiceHealth,
  type ServiceHealthStatus,
} from "../lib/dashboard-data"

const statusTokens: Record<
  ServiceHealthStatus,
  { label: string; dot: string; description: string }
> = {
  online: {
    label: "Online",
    dot: "bg-green-600",
    description: "Operating normally",
  },
  warning: {
    label: "Warning",
    dot: "bg-amber-500",
    description: "Functioning but degraded",
  },
  offline: {
    label: "Offline",
    dot: "bg-red-600",
    description: "Not responding to health checks",
  },
  unknown: {
    label: "Unknown",
    dot: "bg-gray-500",
    description: "Awaiting status update",
  },
}

export default function Page() {
  const [activeView, setActiveView] = useState<"overview" | "investigation">("overview")
  const [alerts, setAlerts] = useState<AlertRecord[]>([])
  const [events, setEvents] = useState<EventLog[]>([])
  const [serviceStatuses, setServiceStatuses] = useState<ServiceHealth[]>([])
  const [loading, setLoading] = useState(true)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)
  const isInitialLoad = loading && !lastUpdated
  const refreshDisabled = loading || isRefreshing
  const lastUpdatedText = lastUpdated?.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }) ?? null
  const lastUpdatedDisplay = lastUpdatedText ?? (loading ? "Loading data..." : "Awaiting first update")

  const getErrorMessage = useCallback((err: unknown) => {
    if (err instanceof Error) {
      return err.message
    }
    return "Failed to load dashboard data"
  }, [])

  const loadDashboardData = useCallback(
    async (signal?: AbortSignal) => {
      const [alertsData, eventsData, statusData] = await Promise.all([
        fetchLatestAlerts(),
        fetchEventLogs(),
        fetchServiceHealth(),
      ])

      if (signal?.aborted) {
        return
      }

      setAlerts(alertsData)
      setEvents(eventsData)
      setServiceStatuses(statusData)
      setLastUpdated(new Date())
    },
    []
  )

  useEffect(() => {
    const controller = new AbortController()

    ;(async () => {
      try {
        await loadDashboardData(controller.signal)
        setError(null)
      } catch (err) {
        if (!controller.signal.aborted) {
          setError(getErrorMessage(err))
        }
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
        }
      }
    })()

    return () => {
      controller.abort()
    }
  }, [getErrorMessage, loadDashboardData])

  const handleRefresh = useCallback(async () => {
    setIsRefreshing(true)
    try {
      await loadDashboardData()
      setError(null)
    } catch (err) {
      setError(getErrorMessage(err))
    } finally {
      setIsRefreshing(false)
    }
  }, [getErrorMessage, loadDashboardData])

  const heading = activeView === "overview" ? "Security Overview" : "Event Investigation"

  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <header className="flex h-16 shrink-0 items-center gap-2 transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-12">
          <div className="flex items-center gap-2 px-4">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-2 data-[orientation=vertical]:h-4" />
            <Breadcrumb>
              <BreadcrumbList>
                <BreadcrumbItem className="hidden md:block">
                  <BreadcrumbLink href="#">ShieldX Console</BreadcrumbLink>
                </BreadcrumbItem>
                <BreadcrumbSeparator className="hidden md:block" />
                <BreadcrumbItem>
                  <BreadcrumbPage>{heading}</BreadcrumbPage>
                </BreadcrumbItem>
              </BreadcrumbList>
            </Breadcrumb>
          </div>
        </header>
        <div className="flex w-full flex-wrap items-start justify-between gap-4 px-4 pb-4 pt-6">
          <div className="space-y-1">
            <p className="text-2xl font-semibold text-foreground">{heading}</p>
            <p className="text-sm text-muted-foreground">
              Monitor real-time posture and triage alerts across ShieldX services
            </p>
            <p className="text-xs text-muted-foreground">{lastUpdatedDisplay}</p>
          </div>
          <div className="flex flex-col items-end gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <Button
                variant={activeView === "overview" ? "default" : "outline"}
                onClick={() => setActiveView("overview")}
              >
                Dashboard
              </Button>
              <Button
                variant={activeView === "investigation" ? "default" : "outline"}
                onClick={() => setActiveView("investigation")}
              >
                Event Investigation
              </Button>
            </div>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handleRefresh}
                disabled={refreshDisabled}
              >
                {isRefreshing ? "Refreshing..." : "Refresh Data"}
              </Button>
            </div>
          </div>
        </div>
        {error && (
          <div className="mx-4 -mt-2 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-100">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <span>{error}</span>
              <Button
                variant="outline"
                size="sm"
                onClick={handleRefresh}
                disabled={isRefreshing}
              >
                Retry
              </Button>
            </div>
          </div>
        )}
        <div className="flex flex-1 flex-col gap-6 px-4 pb-8">
          {isInitialLoad ? (
            <LoadingState />
          ) : activeView === "overview" ? (
            <DashboardOverview alerts={alerts} serviceStatuses={serviceStatuses} />
          ) : (
            <EventInvestigation events={events} />
          )}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}

type DashboardOverviewProps = {
  alerts: AlertRecord[]
  serviceStatuses: ServiceHealth[]
}

function DashboardOverview({ alerts, serviceStatuses }: DashboardOverviewProps) {
  return (
    <div className="flex flex-col gap-6">
      <div className="grid items-stretch gap-4 md:grid-cols-3">
        <div className="flex flex-col gap-4 rounded-xl bg-muted/50 p-4">
          <div className="flex items-center gap-3">
            <div className="rounded-full bg-blue-500/15 p-2 text-blue-500">
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={1.5}
                stroke="currentColor"
                className="size-8"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M12 21a9.004 9.004 0 0 0 8.716-6.747M12 21a9.004 9.004 0 0 1-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 0 1 7.843 4.582M12 3a8.997 8.997 0 0 0-7.843 4.582m15.686 0A11.953 11.953 0 0 1 12 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0 1 21 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0 1 12 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 0 1 3 12c0-1.605.42-3.113 1.157-4.418"
                />
              </svg>
            </div>
            <div className="flex flex-col">
              <span className="text-xs uppercase tracking-wide text-muted-foreground">Total Requests</span>
              <span className="text-3xl font-semibold text-foreground">248,312</span>
            </div>
          </div>
          <ChartLineDefault />
        </div>
        <div className="flex flex-col gap-4 rounded-xl bg-muted/50 p-4">
          <div className="flex items-center gap-3">
            <div className="rounded-full bg-red-500/15 p-2 text-red-500">
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={1.5}
                stroke="currentColor"
                className="size-8"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M12 12.75c1.148 0 2.278.08 3.383.237 1.037.146 1.866.966 1.866 2.013 0 3.728-2.35 6.75-5.25 6.75S6.75 18.728 6.75 15c0-1.046.83-1.867 1.866-2.013A24.204 24.204 0 0 1 12 12.75Zm0 0c2.883 0 5.647.508 8.207 1.44a23.91 23.91 0 0 1-1.152 6.06M12 12.75c-2.883 0-5.647.508-8.208 1.44.125 2.104.52 4.136 1.153 6.06M12 12.75a2.25 2.25 0 0 0 2.248-2.354M12 12.75a2.25 2.25 0 0 1-2.248-2.354M12 8.25c.995 0 1.971-.08 2.922-.236.403-.066.74-.358.795-.762a3.778 3.778 0 0 0-.399-2.25M12 8.25c-.995 0-1.97-.08-2.922-.236-.402-.066-.74-.358-.795-.762a3.734 3.734 0 0 1 .4-2.253M12 8.25a2.25 2.25 0 0 0-2.248 2.146M12 8.25a2.25 2.25 0 0 1 2.248 2.146M8.683 5a6.032 6.032 0 0 1-1.155-1.002c.07-.63.27-1.222.574-1.747m.581 2.749A3.75 3.75 0 0 1 15.318 5m0 0c.427-.283.815-.62 1.155-.999a4.471 4.471 0 0 0-.575-1.752M4.921 6a24.048 24.048 0 0 0-.392 3.314c1.668.546 3.416.914 5.223 1.082M19.08 6c.205 1.08.337 2.187.392 3.314a23.882 23.882 0 0 1-5.223 1.082"
                />
              </svg>
            </div>
            <div className="flex flex-col">
              <span className="text-xs uppercase tracking-wide text-muted-foreground">Threats Blocked</span>
              <span className="text-3xl font-semibold text-foreground">1,482</span>
            </div>
          </div>
          <ChartPieInteractive />
        </div>
        <div className="flex flex-col gap-4 rounded-xl bg-muted/50 p-4">
          <div className="flex items-center gap-3">
            <div className="rounded-full bg-indigo-500/15 p-2 text-indigo-500">
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={1.5}
                stroke="currentColor"
                className="size-8"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M15.59 14.37a6 6 0 0 1-5.84 7.38v-4.8m5.84-2.58a14.98 14.98 0 0 0 6.16-12.12A14.98 14.98 0 0 0 9.631 8.41m5.96 5.96a14.926 14.926 0 0 1-5.841 2.58m-.119-8.54a6 6 0 0 0-7.381 5.84h4.8m2.581-5.84a14.927 14.927 0 0 0-2.58 5.84m2.699 2.7c-.103.021-.207.041-.311.06a15.09 15.09 0 0 1-2.448-2.448 14.9 14.9 0 0 1 .06-.312m-2.24 2.39a4.493 4.493 0 0 0-1.757 4.306 4.493 4.493 0 0 0 4.306-1.758M16.5 9a1.5 1.5 0 1 1-3 0 1.5 1.5 0 0 1 3 0Z"
                />
              </svg>
            </div>
            <div className="flex flex-col">
              <span className="text-xs uppercase tracking-wide text-muted-foreground">Avg. Latency</span>
              <span className="text-3xl font-semibold text-foreground">212 ms</span>
            </div>
          </div>
          <AvgLatency />
        </div>
      </div>

      <div className="grid gap-4 xl:grid-cols-[2fr_1fr]">
        <Card className="bg-muted/50">
          <CardHeader className="gap-4">
            <CardTitle>System Services Health Status</CardTitle>
            <CardDescription>Operational snapshot from the latest health checks</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-6">
            <ServiceStatusLegend />
            <ServiceStatusGrid statuses={serviceStatuses} />
          </CardContent>
        </Card>
        <LatestAlerts alerts={alerts} />
      </div>

      <Card className="bg-muted/50">
        <CardHeader className="gap-1">
          <CardTitle>Traffic &amp; Attack Trends</CardTitle>
          <CardDescription>Rolling activity across protected services</CardDescription>
        </CardHeader>
        <CardContent>
          <ChartAreaInteractive />
        </CardContent>
      </Card>
    </div>
  )
}

function LoadingState() {
  return (
    <div className="rounded-xl border border-dashed border-border/60 bg-muted/30 p-8 text-center text-sm text-muted-foreground">
      Fetching the latest telemetry...
    </div>
  )
}

function ServiceStatusLegend() {
  return (
    <div className="flex flex-wrap gap-4 text-sm text-muted-foreground">
      {(Object.entries(statusTokens) as [ServiceHealthStatus, typeof statusTokens[keyof typeof statusTokens]][]).map(
        ([key, meta]) => (
          <Tooltip key={key}>
            <TooltipTrigger className="flex items-center gap-2">
              <span className={`size-3 rounded-full ${meta.dot}`} aria-hidden />
              <span className="font-medium">{meta.label}</span>
            </TooltipTrigger>
            <TooltipContent>
              <p>{meta.description}</p>
            </TooltipContent>
          </Tooltip>
        )
      )}
    </div>
  )
}

type ServiceStatusGridProps = {
  statuses: ServiceHealth[]
}

function ServiceStatusGrid({ statuses }: ServiceStatusGridProps) {
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
      {statuses.map((service) => (
        <div
          key={service.name}
          className="flex flex-col gap-2 rounded-lg border border-border bg-background/50 p-4 shadow-sm"
        >
          <div className="flex items-center justify-between gap-2">
            <span className="text-sm font-semibold text-foreground">{service.name}</span>
            <span className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              <span className={`size-2.5 rounded-full ${statusTokens[service.status].dot}`} aria-hidden />
              {statusTokens[service.status].label}
            </span>
          </div>
          <p className="text-xs text-muted-foreground">{service.summary}</p>
          {typeof service.latencyMs === "number" && (
            <span className="text-xs text-muted-foreground">
              Median latency: {service.latencyMs} ms
            </span>
          )}
        </div>
      ))}
    </div>
  )
}
