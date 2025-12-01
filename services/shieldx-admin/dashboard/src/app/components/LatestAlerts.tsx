import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export type AlertRecord = {
  id: string
  service: string
  severity: "critical" | "high" | "medium" | "low"
  category: string
  status: "blocked" | "mitigated" | "in_progress" | "resolved"
  summary: string
  timestamp: string
}

type LatestAlertsProps = {
  alerts: AlertRecord[]
}

const severityConfig: Record<AlertRecord["severity"], string> = {
  critical: "bg-red-500/15 text-red-500",
  high: "bg-amber-500/15 text-amber-600",
  medium: "bg-blue-500/15 text-blue-500",
  low: "bg-gray-500/10 text-gray-500",
}

const statusLabel: Record<AlertRecord["status"], string> = {
  blocked: "Blocked",
  mitigated: "Mitigated",
  in_progress: "Investigating",
  resolved: "Resolved",
}

export function LatestAlerts({ alerts }: LatestAlertsProps) {
  return (
    <Card className="bg-muted/50">
      <CardHeader className="gap-1">
        <CardTitle className="text-lg">Latest High-Severity Alerts</CardTitle>
        <CardDescription>
          Ten most recent critical and high alerts from the last 24 hours
        </CardDescription>
      </CardHeader>
      <CardContent>
        {alerts.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">No high-severity alerts available.</p>
        ) : (
          <div className="flex flex-col divide-y divide-border">
            {alerts.map((alert) => (
              <div
                key={alert.id}
                className="flex flex-col gap-2 py-4 first:pt-0 last:pb-0 md:flex-row md:items-center md:justify-between"
              >
                <div className="flex flex-col gap-1">
                  <div className="flex items-center gap-3">
                    <span
                      className={`rounded-full px-2 py-0.5 text-xs font-semibold uppercase tracking-wide ${severityConfig[alert.severity]}`}
                    >
                      {alert.severity}
                    </span>
                    <span className="text-xs uppercase tracking-wide text-muted-foreground">
                      {alert.category}
                    </span>
                  </div>
                  <p className="text-sm font-medium text-foreground">
                    {alert.summary}
                  </p>
                  <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                    <span>{alert.service}</span>
                    <span className="hidden h-1 w-1 rounded-full bg-muted md:inline-flex" aria-hidden />
                    <span>{new Date(alert.timestamp).toLocaleString()}</span>
                  </div>
                </div>
                <div className="flex items-center justify-between gap-4 md:flex-col md:items-end">
                  <span className="rounded-md bg-background px-3 py-1 text-xs font-medium text-muted-foreground shadow-sm">
                    {statusLabel[alert.status]}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
