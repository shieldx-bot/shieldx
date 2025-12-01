import type { AlertRecord } from "@/app/components/LatestAlerts"
import type { EventLog } from "@/app/components/EventInvestigation"

export type ServiceHealthStatus = "online" | "warning" | "offline" | "unknown"

export type ServiceHealth = {
  name: string
  status: ServiceHealthStatus
  summary: string
  latencyMs?: number
}

const latencyRange = { min: 120, max: 480 }

const delay = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

const randomLatency = () =>
  latencyRange.min + Math.floor(Math.random() * (latencyRange.max - latencyRange.min))

export async function fetchLatestAlerts(): Promise<AlertRecord[]> {
  await delay(randomLatency())
  return buildAlerts()
}

export async function fetchEventLogs(): Promise<EventLog[]> {
  await delay(randomLatency())
  return buildEvents()
}

export async function fetchServiceHealth(): Promise<ServiceHealth[]> {
  await delay(randomLatency())
  return buildServiceHealth()
}

function buildAlerts(): AlertRecord[] {
  const now = Date.now()
  const minutesAgo = (min: number) => new Date(now - min * 60 * 1000).toISOString()

  return [
    {
      id: "al-001",
      service: "shieldx-gateway",
      severity: "critical",
      category: "API Firewall",
      status: "blocked",
      summary: "SQL injection attempt blocked on /api/v1/billing",
      timestamp: minutesAgo(5),
    },
    {
      id: "al-002",
      service: "shieldx-auth",
      severity: "high",
      category: "Authentication",
      status: "mitigated",
      summary: "Suspicious login burst from shared ASN throttled",
      timestamp: minutesAgo(12),
    },
    {
      id: "al-003",
      service: "shieldx-policy",
      severity: "high",
      category: "Policy",
      status: "in_progress",
      summary: "Policy sync delayed due to long-running evaluation",
      timestamp: minutesAgo(25),
    },
    {
      id: "al-004",
      service: "shieldx-guardian",
      severity: "critical",
      category: "Sandbox",
      status: "blocked",
      summary: "Malware payload quarantined during sandbox detonation",
      timestamp: minutesAgo(38),
    },
    {
      id: "al-005",
      service: "shieldx-credits",
      severity: "medium",
      category: "Rate Limiting",
      status: "mitigated",
      summary: "User exceeded per-minute allocation, automatic throttle applied",
      timestamp: minutesAgo(44),
    },
    {
      id: "al-006",
      service: "shieldx-ml",
      severity: "high",
      category: "Behavioral Detection",
      status: "blocked",
      summary: "Anomaly score spiked for tenant-23 API usage",
      timestamp: minutesAgo(51),
    },
    {
      id: "al-007",
      service: "shieldx-gateway",
      severity: "critical",
      category: "DDoS",
      status: "mitigated",
      summary: "Edge network absorbed volumetric traffic from 3 ASNs",
      timestamp: minutesAgo(65),
    },
    {
      id: "al-008",
      service: "shieldx-deception",
      severity: "medium",
      category: "Deception",
      status: "blocked",
      summary: "Honey service interacted with credential harvesting script",
      timestamp: minutesAgo(72),
    },
    {
      id: "al-009",
      service: "shieldx-forensics",
      severity: "high",
      category: "Forensics",
      status: "in_progress",
      summary: "Artifact pipeline backlog detected during memory image analysis",
      timestamp: minutesAgo(86),
    },
    {
      id: "al-010",
      service: "shieldx-admin",
      severity: "medium",
      category: "Dashboard",
      status: "resolved",
      summary: "Admin session flagged as idle and forcefully logged out",
      timestamp: minutesAgo(110),
    },
  ]
}

function buildEvents(): EventLog[] {
  const now = Date.now()
  const minutesAgo = (min: number) => new Date(now - min * 60 * 1000).toISOString()
  const hoursAgo = (hrs: number) => new Date(now - hrs * 60 * 60 * 1000).toISOString()

  return [
    {
      id: "ev-001",
      timestamp: minutesAgo(7),
      severity: "critical",
      service: "shieldx-gateway",
      attackType: "SQL Injection",
      sourceIP: "203.0.113.45",
      destination: "api.shieldx.cloud/billing",
      status: "blocked",
      description: "Blocked injected payload targeting payment endpoint",
    },
    {
      id: "ev-002",
      timestamp: minutesAgo(13),
      severity: "high",
      service: "shieldx-auth",
      attackType: "Credential Stuffing",
      sourceIP: "198.51.100.12",
      destination: "auth.shieldx.cloud/login",
      status: "mitigated",
      description: "Rate limiter throttled repeated login attempts",
    },
    {
      id: "ev-003",
      timestamp: minutesAgo(21),
      severity: "medium",
      service: "shieldx-policy",
      attackType: "Policy Drift",
      sourceIP: "10.15.8.40",
      destination: "policy.shieldx.cloud/sync",
      status: "allowed",
      description: "Sync delay detected, auto-retry scheduled",
    },
    {
      id: "ev-004",
      timestamp: minutesAgo(29),
      severity: "critical",
      service: "shieldx-guardian",
      attackType: "Malware Detonation",
      sourceIP: "178.33.19.5",
      destination: "guardian.shieldx.cloud/analyse",
      status: "blocked",
      description: "Sandbox isolated ransomware sample during execution",
    },
    {
      id: "ev-005",
      timestamp: minutesAgo(36),
      severity: "high",
      service: "shieldx-credits",
      attackType: "Abuse of Allowance",
      sourceIP: "192.0.2.90",
      destination: "credits.shieldx.cloud/usage",
      status: "mitigated",
      description: "Tenant exceeded request quota and was throttled",
    },
    {
      id: "ev-006",
      timestamp: minutesAgo(48),
      severity: "medium",
      service: "shieldx-ml",
      attackType: "Behavioral Anomaly",
      sourceIP: "10.3.7.12",
      destination: "ml.shieldx.cloud/infer",
      status: "blocked",
      description: "Inference flagged due to anomaly score greater than 0.82",
    },
    {
      id: "ev-007",
      timestamp: hoursAgo(2),
      severity: "high",
      service: "shieldx-gateway",
      attackType: "DDoS",
      sourceIP: "Multiple",
      destination: "edge.shieldx.cloud",
      status: "mitigated",
      description: "Edge shield absorbed 1.4M requests per minute",
    },
    {
      id: "ev-008",
      timestamp: hoursAgo(3),
      severity: "low",
      service: "shieldx-admin",
      attackType: "Session Timeout",
      sourceIP: "192.0.2.4",
      destination: "admin.shieldx.cloud",
      status: "allowed",
      description: "Session expired after inactivity limit",
    },
    {
      id: "ev-009",
      timestamp: hoursAgo(4),
      severity: "medium",
      service: "shieldx-deception",
      attackType: "Credential Harvest",
      sourceIP: "203.0.113.99",
      destination: "decoy.shieldx.cloud",
      status: "blocked",
      description: "Attacker interacted with honey credential store",
    },
    {
      id: "ev-010",
      timestamp: hoursAgo(5.5),
      severity: "high",
      service: "shieldx-forensics",
      attackType: "Artifact Pipeline",
      sourceIP: "172.22.41.8",
      destination: "forensics.shieldx.cloud",
      status: "mitigated",
      description: "Processing queue rerouted to secondary worker",
    },
    {
      id: "ev-011",
      timestamp: hoursAgo(8),
      severity: "low",
      service: "shieldx-sandbox",
      attackType: "Scheduled Execution",
      sourceIP: "scheduler",
      destination: "sandbox.shieldx.cloud",
      status: "allowed",
      description: "Routine sandbox job completed",
    },
    {
      id: "ev-012",
      timestamp: hoursAgo(12),
      severity: "medium",
      service: "shieldx-policy",
      attackType: "OPA Evaluation",
      sourceIP: "10.4.1.52",
      destination: "policy.shieldx.cloud/eval",
      status: "allowed",
      description: "OPA bundle refreshed with new rules",
    },
  ]
}

function buildServiceHealth(): ServiceHealth[] {
  return [
    {
      name: "Admin",
      status: "online",
      summary: "Dashboards responding normally",
      latencyMs: 95,
    },
    {
      name: "Auth",
      status: "online",
      summary: "Median login latency stable",
      latencyMs: 112,
    },
    {
      name: "Credits",
      status: "unknown",
      summary: "Awaiting consumption telemetry",
    },
    {
      name: "Deception",
      status: "unknown",
      summary: "No honeypot alerts in last 6 hours",
    },
    {
      name: "Forensics",
      status: "offline",
      summary: "Artifact processor queue stalled",
    },
    {
      name: "Gateway",
      status: "unknown",
      summary: "Partial metrics from edge collectors",
    },
    {
      name: "ML",
      status: "online",
      summary: "Latest models deployed across regions",
      latencyMs: 180,
    },
    {
      name: "Policy",
      status: "warning",
      summary: "OPA sync lagging behind desired interval",
      latencyMs: 420,
    },
    {
      name: "Sandbox",
      status: "unknown",
      summary: "Next detonation window scheduled in 25 minutes",
    },
  ]
}
