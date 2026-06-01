import { ResourceSample } from './models';

const byteUnits: Record<string, number> = {
  b: 1,
  kb: 1000,
  mb: 1000 * 1000,
  gb: 1000 * 1000 * 1000,
  kib: 1024,
  mib: 1024 * 1024,
  gib: 1024 * 1024 * 1024
};

export interface StatsAggregate {
  available: boolean;
  containerCount: number;
  cpuPercentHostTotal: number;
  memoryBytes: number;
  memoryLimitBytes: number;
  sampledAt: string;
  singleMemoryRaw: string;
}

export function aggregateResourceSamples(samples: ResourceSample[]): StatsAggregate {
  const current = samples.filter((sample) => hasCurrentStats(sample));
  let sampledAt = '';
  const aggregate = current.reduce(
    (total, sample) => {
      total.cpuPercentHostTotal += sample.cpu_percent_host_total || 0;
      total.memoryBytes += sample.memory_bytes || 0;
      total.memoryLimitBytes += parseMemoryLimitBytes(sample.memory_podman_raw);
      if (!sampledAt || Date.parse(sample.sampled_at) > Date.parse(sampledAt)) {
        sampledAt = sample.sampled_at;
      }
      return total;
    },
    { cpuPercentHostTotal: 0, memoryBytes: 0, memoryLimitBytes: 0 }
  );

  return {
    available: current.length > 0,
    containerCount: current.length,
    cpuPercentHostTotal: aggregate.cpuPercentHostTotal,
    memoryBytes: aggregate.memoryBytes,
    memoryLimitBytes: aggregate.memoryLimitBytes,
    sampledAt,
    singleMemoryRaw: current.length === 1 ? current[0].memory_podman_raw : ''
  };
}

export function hasCurrentStats(sample: ResourceSample): boolean {
  return sample.memory_podman_raw !== '0B / 0B' && (sample.memory_bytes > 0 || sample.cpu_podman_raw !== '');
}

export function cpuProgressValue(aggregate: StatsAggregate): number {
  return clampPercent(aggregate.cpuPercentHostTotal);
}

export function memoryProgressValue(aggregate: StatsAggregate): number {
  if (aggregate.memoryLimitBytes <= 0) {
    return 0;
  }
  return clampPercent((aggregate.memoryBytes / aggregate.memoryLimitBytes) * 100);
}

export function formatCpuPercent(value: number): string {
  if (value === 0) {
    return '0.0%';
  }
  if (value < 0.01) {
    return `${value.toFixed(3)}%`;
  }
  if (value < 0.1) {
    return `${value.toFixed(2)}%`;
  }
  return `${value.toFixed(1)}%`;
}

export function formatMemoryDisplay(aggregate: StatsAggregate): string {
  return formatBytes(aggregate.memoryBytes);
}

export function formatBytes(bytes = 0): string {
  if (bytes < 1024) {
    return `${bytes.toFixed(0)} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KiB`;
  }
  if (bytes < 1024 * 1024 * 1024) {
    return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
  }
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GiB`;
}

export function parseMemoryLimitBytes(raw: string): number {
  const parts = raw.split('/');
  if (parts.length < 2) {
    return 0;
  }
  return parseMemoryBytes(parts[1]);
}

function parseMemoryBytes(raw: string): number {
  const match = raw.trim().match(/^([0-9]+(?:\.[0-9]+)?)\s*([A-Za-z]+)$/);
  if (!match) {
    return 0;
  }
  const value = Number.parseFloat(match[1]);
  const factor = byteUnits[match[2].toLowerCase()] ?? 0;
  return factor > 0 ? value * factor : 0;
}

function clampPercent(value: number): number {
  return Math.min(100, Math.max(0, value));
}
