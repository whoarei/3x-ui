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
    const intervalAt = (timestamp: number) => {
      let low = 0;
      let high = points.length;
      while (low < high) {
        const middle = (low + high) >>> 1;
        if (points[middle].end <= timestamp) low = middle + 1;
        else high = middle;
      }
      return low < points.length && timestamp >= points[low].start ? low : null;
    };
    const data: uPlot.AlignedData = [
      points.map((p) => (p.start + p.end) / 2),
      points.map((p) => (p.coveredMinutes || p.up || p.down ? p.down : null)),
      points.map((p) => (p.coveredMinutes || p.up || p.down ? p.up + p.down : null)),
    ];
    const bars: uPlot.Series.PathBuilder = (u, seriesIndex, first, last) => {
      const fill = new Path2D();
      for (let index = first; index <= last; index++) {
        const value = u.data[seriesIndex][index];
        if (value == null) continue;
        const point = points[index];
        const left = u.valToPos(point.start, 'x', true);
        const right = u.valToPos(point.end, 'x', true);
        const width = Math.min((right - left) * 0.72, 48 * uPlot.pxRatio);
        const amount = seriesIndex === 1 ? point.down : point.up;
        const baseline = seriesIndex === 1 || !u.series[1].show ? 0 : point.down;
        const bottom = u.valToPos(baseline, 'y', true);
        const top = u.valToPos(baseline + amount, 'y', true);
        fill.rect((left + right - width) / 2, top, width, bottom - top);
      }
      return { fill };
    };
    const plot = new uPlot(
      {
        width: Math.max(280, container.clientWidth),
        height: 320,
        padding: [16, 16, 0, 0],
        scales: {
          x: { range: () => [points[0].start, points[points.length - 1].end] },
          y: {
            range: (u) => [
              0,
              Math.max(
                1024,
                points.reduce(
                  (max, p) =>
                    Math.max(max, (u.series[1].show ? p.down : 0) + (u.series[2].show ? p.up : 0)),
                  0,
                ) * 1.15,
              ),
            ],
          },
        },
        legend: { show: true, live: true },
        series: [
          {
            label: t('trafficHistory.range'),
            value: (_u, _ts, _series, index) =>
              index == null ? '—' : `${format(points[index].start)} – ${format(points[index].end)}`,
          },
          {
            label: t('trafficHistory.download'),
            stroke: '#13a8a8',
            fill: '#13a8a8',
            width: 0,
            paths: bars,
            points: { show: false },
            value: (_u, n) => (n == null ? '—' : bytes(n)),
          },
          {
            label: t('trafficHistory.upload'),
            stroke: '#597ef7',
            fill: '#597ef7',
            width: 0,
            paths: bars,
            points: { show: false },
            value: (_u, n, _series, index) =>
              n == null || index == null ? '—' : bytes(points[index].up),
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
        cursor: {
          drag: { x: false, y: false },
          points: { show: false },
          dataIdx: (_u, _series, _closest, timestamp) => intervalAt(timestamp),
        },
        hooks: {
          setCursor: [
            (u) => {
              const el = tooltip.current;
              const index = u.cursor.idxs?.[1];
              const p = index == null ? undefined : points[index];
              if (!el || !p || (u.cursor.left ?? -1) < 0) {
                if (el) el.hidden = true;
                return;
              }
              el.hidden = false;
              el.textContent = `${format(p.start)} – ${format(p.end)}\n${t('trafficHistory.download')}: ${bytes(p.down)}\n${t('trafficHistory.upload')}: ${bytes(p.up)}\n${t('trafficHistory.total')}: ${bytes(p.up + p.down)}${p.coveredMinutes < p.expectedMinutes ? `\n${t('trafficHistory.gaps')}` : ''}`;
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
