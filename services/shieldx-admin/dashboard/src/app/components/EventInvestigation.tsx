import { useMemo, useState } from "react"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

export type EventLog = {
  id: string
  timestamp: string
  severity: "critical" | "high" | "medium" | "low"
  service: string
  attackType: string
  sourceIP: string
  destination: string
  status: "blocked" | "allowed" | "mitigated"
  description: string
}

type EventInvestigationProps = {
  events: EventLog[]
}

type FilterState = {
  search: string
  severity: "all" | EventLog["severity"]
  service: "all" | string
  timeframe: "1h" | "6h" | "24h" | "7d" | "all"
}

const severityStyles: Record<
  EventLog["severity"],
  { badge: string; dot: string }
> = {
  critical: {
    badge: "bg-red-500/15 text-red-500",
    dot: "bg-red-500",
  },
  high: {
    badge: "bg-amber-500/15 text-amber-600",
    dot: "bg-amber-500",
  },
  medium: {
    badge: "bg-blue-500/15 text-blue-500",
    dot: "bg-blue-500",
  },
  low: {
    badge: "bg-gray-500/10 text-gray-500",
    dot: "bg-gray-500",
  },
}

const timeframeToMs: Record<Exclude<FilterState["timeframe"], "all">, number> = {
  "1h": 60 * 60 * 1000,
  "6h": 6 * 60 * 60 * 1000,
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
}

const defaultFilters: FilterState = {
  search: "",
  severity: "all",
  service: "all",
  timeframe: "24h",
}

export function EventInvestigation({ events }: EventInvestigationProps) {
  const [filters, setFilters] = useState<FilterState>(defaultFilters)

  const services = useMemo(() => {
    const catalog = new Set<string>()
    events.forEach((event) => catalog.add(event.service))
    return Array.from(catalog).sort()
  }, [events])

  const stats = useMemo(() => {
    return events.reduce(
      (acc, event) => {
        acc.total += 1
        acc.bySeverity[event.severity] += 1
        return acc
      },
      {
        total: 0,
        bySeverity: {
          critical: 0,
          high: 0,
          medium: 0,
          low: 0,
        },
      }
    )
  }, [events])

  const filteredEvents = useMemo(() => {
    const now = Date.now()
    const query = filters.search.trim().toLowerCase()

    return events
      .filter((event) => {
        if (filters.severity !== "all" && event.severity !== filters.severity) {
          return false
        }

        if (filters.service !== "all" && event.service !== filters.service) {
          return false
        }

        if (filters.timeframe !== "all") {
          const ms = timeframeToMs[filters.timeframe]
          const eventTimestamp = new Date(event.timestamp).getTime()
          if (!Number.isFinite(eventTimestamp) || now - eventTimestamp > ms) {
            return false
          }
        }

        if (!query) {
          return true
        }

        const haystack = [
          event.service,
          event.attackType,
          event.sourceIP,
          event.destination,
          event.description,
        ]
          .join(" ")
          .toLowerCase()

        return haystack.includes(query)
      })
      .sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
  }, [events, filters])

  return (
    <div className="flex flex-col gap-6">
      <div className="grid gap-4 md:grid-cols-4">
        <Card className="bg-muted/50">
          <CardHeader className="gap-1">
            <CardTitle className="text-2xl font-semibold">{stats.total}</CardTitle>
            <CardDescription>Total events in selection</CardDescription>
          </CardHeader>
        </Card>
        {(["critical", "high", "medium", "low"] as const).map((severity) => (
          <Card key={severity} className="bg-muted/50">
            <CardHeader className="gap-1">
              <CardTitle className="flex items-center gap-2 text-base font-semibold capitalize">
                <span className={`size-2.5 rounded-full ${severityStyles[severity].dot}`} aria-hidden />
                {severity}
              </CardTitle>
              <CardDescription>{stats.bySeverity[severity]} events</CardDescription>
            </CardHeader>
          </Card>
        ))}
      </div>

      <Card className="bg-muted/50">
        <CardHeader className="gap-1">
          <CardTitle>Search &amp; Filters</CardTitle>
          <CardDescription>Investigate events with live filtering</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <Input
              placeholder="Search by IP, attack type, destination..."
              value={filters.search}
              onChange={(event) => setFilters((prev) => ({ ...prev, search: event.target.value }))}
            />
            <Select
              value={filters.severity}
              onValueChange={(value) =>
                setFilters((prev) => ({ ...prev, severity: value as FilterState["severity"] }))
              }
            >
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Severity" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All severities</SelectItem>
                <SelectItem value="critical">Critical</SelectItem>
                <SelectItem value="high">High</SelectItem>
                <SelectItem value="medium">Medium</SelectItem>
                <SelectItem value="low">Low</SelectItem>
              </SelectContent>
            </Select>
            <Select
              value={filters.service}
              onValueChange={(value) =>
                setFilters((prev) => ({ ...prev, service: value as FilterState["service"] }))
              }
            >
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Service" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All services</SelectItem>
                {services.map((service) => (
                  <SelectItem key={service} value={service}>
                    {service}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={filters.timeframe}
              onValueChange={(value) =>
                setFilters((prev) => ({ ...prev, timeframe: value as FilterState["timeframe"] }))
              }
            >
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Time range" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="1h">Past hour</SelectItem>
                <SelectItem value="6h">Past 6 hours</SelectItem>
                <SelectItem value="24h">Past 24 hours</SelectItem>
                <SelectItem value="7d">Past 7 days</SelectItem>
                <SelectItem value="all">All time</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>

      <Card className="bg-muted/50">
        <CardHeader className="gap-1">
          <CardTitle>Event Timeline</CardTitle>
          <CardDescription>
            Ordered by newest first — includes security actions and impacted endpoints
          </CardDescription>
        </CardHeader>
        <CardContent className="overflow-x-auto">
          {filteredEvents.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              No events match the current filters.
            </p>
          ) : (
            <table className="min-w-full table-fixed border-collapse text-sm">
              <thead>
                <tr className="text-left text-muted-foreground">
                  <th className="w-40 border-b border-border px-4 py-3 font-medium">Timestamp</th>
                  <th className="w-28 border-b border-border px-4 py-3 font-medium">Severity</th>
                  <th className="w-40 border-b border-border px-4 py-3 font-medium">Service</th>
                  <th className="w-48 border-b border-border px-4 py-3 font-medium">Attack Type</th>
                  <th className="w-40 border-b border-border px-4 py-3 font-medium">Source IP</th>
                  <th className="w-40 border-b border-border px-4 py-3 font-medium">Destination</th>
                  <th className="w-32 border-b border-border px-4 py-3 font-medium">Status</th>
                  <th className="border-b border-border px-4 py-3 font-medium">Details</th>
                </tr>
              </thead>
              <tbody>
                {filteredEvents.map((event) => (
                  <tr key={event.id} className="border-b border-border last:border-0">
                    <td className="px-4 py-3 text-muted-foreground">
                      {new Date(event.timestamp).toLocaleString()}
                    </td>
                    <td className="px-4 py-3">
                      <span
                        className={`rounded-full px-2 py-0.5 text-xs font-semibold uppercase tracking-wide ${severityStyles[event.severity].badge}`}
                      >
                        {event.severity}
                      </span>
                    </td>
                    <td className="truncate px-4 py-3 font-medium text-foreground">
                      {event.service}
                    </td>
                    <td className="truncate px-4 py-3 text-muted-foreground">
                      {event.attackType}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {event.sourceIP}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {event.destination}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground capitalize">
                      {event.status.replace("_", " ")}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {event.description}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
