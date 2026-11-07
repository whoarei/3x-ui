import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { theme } from 'antd';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';

import type { TrafficHistoryPoint } from '@/generated/types';

const bytes = (n: number) => {
  if (n < 1024) return `${n.toFixed(0)} B`;
  if (n < 1024 ** 2) return `${(n / 1024).toFixed(1)} KiB`;
  if (n < 1024 ** 3) return `${(n / 1024 ** 2).toFixed(1)} MiB`;
  return `${(n / 1024 ** 3).toFixed(2)} GiB`;
};

export { bytes as trafficBytes };

export default function TrafficHistoryChart({
  points,
  timeZone,
}: {
  points: TrafficHistoryPoint[];
  timeZone: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const tooltip = useRef<HTMLDivElement>(null);
  const { token } = theme.useToken();
  const { t, i18n } = useTranslation();

  useEffect(() => {
    if (!host.current || !points.length) return;
    const container = host.current;
    const date = new Intl.DateTimeFormat(i18n.language, {
      timeZone,
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
    const format = (ts: number) => date.format(new Date(ts * 1000));
    const data: uPlot.AlignedData = [
      points.map((p) => p.start),
      points.map((p) => (p.coveredMinutes || p.up || p.down ? p.down : null)),
      points.map((p) => (p.coveredMinutes || p.up || p.down ? p.up : null)),
    ];
    const plot = new uPlot(
      {
        width: Math.max(280, container.clientWidth),
        height: 320,
        padding: [16, 16, 0, 0],
        scales: { y: { range: (_u, _min, max) => [0, Math.max(1024, max * 1.15)] } },
        legend: { show: true, live: true },
        series: [
          { label: t('trafficHistory.range'), value: (_u, ts) => (ts == null ? '—' : format(ts)) },
          {
            label: t('trafficHistory.download'),
            stroke: '#13a8a8',
            fill: '#13a8a814',
            width: 2,
            points: { show: true, size: 4 },
            spanGaps: false,
            value: (_u, n) => (n == null ? '—' : bytes(n)),
          },
          {
            label: t('trafficHistory.upload'),
            stroke: '#597ef7',
            width: 2,
            points: { show: true, size: 4 },
            spanGaps: false,
            value: (_u, n) => (n == null ? '—' : bytes(n)),
          },
        ],
        axes: [
          {
            stroke: token.colorTextSecondary,
            grid: { show: false },
            values: (_u, ticks) => ticks.map(format),
            space: 110,
          },
          {
            stroke: token.colorTextSecondary,
            grid: { stroke: token.colorBorderSecondary },
            values: (_u, ticks) => ticks.map(bytes),
            size: 85,
          },
        ],
        cursor: { drag: { x: false, y: false } },
        hooks: {
          setCursor: [
            (u) => {
              const el = tooltip.current;
              const index = u.cursor.idx;
              const p = index == null ? undefined : points[index];
              if (!el || !p || (u.cursor.left ?? -1) < 0) {
                if (el) el.hidden = true;
                return;
              }
              el.hidden = false;
              el.textContent = `${format(p.start)} – ${format(p.end)}\n${t('trafficHistory.download')}: ${bytes(p.down)}\n${t('trafficHistory.upload')}: ${bytes(p.up)}${p.coveredMinutes < p.expectedMinutes ? `\n${t('trafficHistory.gaps')}` : ''}`;
              el.style.left = `${Math.min(Math.max(0, container.clientWidth - 270), Math.max(0, (u.cursor.left ?? 0) + 65))}px`;
            },
          ],
        },
      },
      data,
      container,
    );
    const observer = new ResizeObserver(() =>
      plot.setSize({ width: Math.max(280, container.clientWidth), height: 320 }),
    );
    observer.observe(container);
    return () => {
      observer.disconnect();
      plot.destroy();
    };
  }, [points, timeZone, token.colorTextSecondary, token.colorBorderSecondary, t, i18n.language]);

  return (
    <div className="traffic-chart-wrap">
      <div ref={host} className="traffic-chart" />
      <div ref={tooltip} hidden className="traffic-chart-tooltip" />
    </div>
  );
}
