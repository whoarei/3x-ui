import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Col,
  ConfigProvider,
  DatePicker,
  Empty,
  Layout,
  Row,
  Segmented,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  BarChartOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import dayjs from 'dayjs';
import type { Dayjs } from 'dayjs';
import utc from 'dayjs/plugin/utc';
import timezone from 'dayjs/plugin/timezone';

import { HttpUtil } from '@/utils';
import { useTheme } from '@/hooks/useTheme';
import AppSidebar from '@/layouts/AppSidebar';
import type {
  TrafficHistoryClientTotal,
  TrafficHistoryOptions,
  TrafficHistoryResponse,
} from '@/generated/types';
import TrafficHistoryChart, { trafficBytes } from './TrafficHistoryChart';
import DomainTrafficPanel from './DomainTrafficPanel';
import './TrafficHistoryPage.css';

dayjs.extend(utc);
dayjs.extend(timezone);

type Period = '1' | '7' | '30' | 'custom';
const recent = (days: number) => {
  const to = Math.floor(Date.now() / 1000);
  return { from: to - days * 86400, to };
};

export default function TrafficHistoryPage() {
  const { t, i18n } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [params, setParams] = useSearchParams();
  const view = params.get('view') === 'sites' ? 'sites' : 'clients';
  const [refreshKey, setRefreshKey] = useState(0);
  const rawId = Number(params.get('clientId') ?? 0);
  const clientId = Number.isSafeInteger(rawId) && rawId > 0 ? rawId : 0;
  const [period, setPeriod] = useState<Period>('1');
  const [range, setRange] = useState(() => recent(1));
  const [custom, setCustom] = useState<[Dayjs, Dayjs] | null>(null);
  const [invalid, setInvalid] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const options = useQuery({
    queryKey: ['clientTrafficHistoryOptions'],
    queryFn: async ({ signal }) => {
      const msg = await HttpUtil.get<TrafficHistoryOptions>(
        '/panel/api/clients/traffic-history/options',
        undefined,
        { signal, silent: true },
      );
      if (!msg.success || !msg.obj) throw new Error(msg.msg || t('somethingWentWrong'));
      return msg.obj;
    },
    refetchInterval: 30_000,
  });
  const timeZone = options.data?.timeZone || 'UTC';
  const history = useQuery({
    queryKey: ['clientTrafficHistory', clientId, range.from, range.to],
    queryFn: async ({ signal }) => {
      const msg = await HttpUtil.get<TrafficHistoryResponse>(
        '/panel/api/clients/traffic-history',
        { clientId, ...range },
        { signal, silent: true },
      );
      if (!msg.success || !msg.obj) throw new Error(msg.msg || t('somethingWentWrong'));
      return msg.obj;
    },
    refetchInterval: period === 'custom' ? false : 30_000,
  });

  useEffect(() => {
    if (period === 'custom') return;
    const timer = window.setInterval(() => setRange(recent(Number(period))), 30_000);
    return () => window.clearInterval(timer);
  }, [period]);

  function selectPeriod(value: Period) {
    setPickerOpen(false);
    setPeriod(value);
    setInvalid(false);
    if (value !== 'custom') setRange(recent(Number(value)));
    else if (!custom)
      setCustom([dayjs.unix(range.from).tz(timeZone), dayjs.unix(range.to).tz(timeZone)]);
  }

  function applyCustom() {
    if (!custom) {
      setInvalid(true);
      return;
    }
    const from = dayjs.tz(custom[0].format('YYYY-MM-DD HH:mm'), timeZone).unix();
    const to = dayjs.tz(custom[1].format('YYYY-MM-DD HH:mm'), timeZone).unix();
    if (to <= from) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    setPickerOpen(false);
    setRange({ from, to });
  }

  function selectClient(id: number) {
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      if (id) next.set('clientId', String(id));
      else next.delete('clientId');
      return next;
    });
  }

  const dateFormat = useMemo(
    () =>
      new Intl.DateTimeFormat(i18n.language, { timeZone, dateStyle: 'medium', timeStyle: 'short' }),
    [i18n.language, timeZone],
  );
  const format = (ts: number) => (ts ? dateFormat.format(new Date(ts * 1000)) : '—');
  const data = history.data;
  const resolutionLabels: Record<string, string> = {
    minute: t('trafficHistory.minute'),
    hour: t('trafficHistory.hour'),
    day: t('trafficHistory.day'),
    week: t('trafficHistory.week'),
    month: t('trafficHistory.month'),
  };
  const missing = data?.points.some((p) => p.coveredMinutes < p.expectedMinutes);
  const clientOptions = [
    { value: 0, label: t('trafficHistory.allClients') },
    ...(options.data?.clients ?? []).map((c) => ({
      value: c.id,
      label: `${c.comment ? `${c.comment} · ` : ''}${c.email}${c.deleted ? ` (${t('trafficHistory.deleted')})` : ''}`,
    })),
  ];
  const columns = [
    {
      title: t('trafficHistory.client'),
      dataIndex: 'email',
      key: 'client',
      render: (_: string, row: TrafficHistoryClientTotal) => (
        <Button type="link" onClick={() => selectClient(row.id)}>
          {row.comment ? `${row.comment} · ${row.email}` : row.email}
          {row.deleted && <Tag>{t('trafficHistory.deleted')}</Tag>}
        </Button>
      ),
    },
    {
      title: t('trafficHistory.download'),
      dataIndex: 'down',
      key: 'down',
      render: trafficBytes,
      sorter: (a: TrafficHistoryClientTotal, b: TrafficHistoryClientTotal) => a.down - b.down,
    },
    {
      title: t('trafficHistory.upload'),
      dataIndex: 'up',
      key: 'up',
      render: trafficBytes,
      sorter: (a: TrafficHistoryClientTotal, b: TrafficHistoryClientTotal) => a.up - b.up,
    },
    {
      title: t('trafficHistory.total'),
      key: 'total',
      render: (_: unknown, row: TrafficHistoryClientTotal) => trafficBytes(row.up + row.down),
      sorter: (a: TrafficHistoryClientTotal, b: TrafficHistoryClientTotal) =>
        a.up + a.down - b.up - b.down,
    },
  ];

  return (
    <ConfigProvider theme={antdThemeConfig}>
      <Layout
        className={`traffic-history-page ${isDark ? 'is-dark' : ''} ${isUltra ? 'is-ultra' : ''}`}
      >
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <div className="traffic-history-stack">
              <div className="traffic-history-heading">
                <Typography.Title level={3}>
                  <BarChartOutlined aria-hidden /> {t('trafficHistory.title')}
                </Typography.Title>
                <Button
                  icon={<ReloadOutlined aria-hidden />}
                  loading={history.isFetching || options.isFetching}
                  onClick={() => {
                    setRefreshKey((n) => n + 1);
                    if (period !== 'custom') setRange(recent(Number(period)));
                    void history.refetch();
                    void options.refetch();
                  }}
                >
                  {t('refresh')}
                </Button>
              </div>
              <Card>
                <Space wrap size="middle" className="traffic-history-filters">
                  <Select
                    aria-label={t('trafficHistory.client')}
                    showSearch={{ optionFilterProp: 'label' }}
                    value={clientId}
                    options={clientOptions}
                    onChange={selectClient}
                    loading={options.isLoading}
                    className="traffic-history-client-select"
                  />
                  <Segmented
                    aria-label={t('trafficHistory.range')}
                    value={period}
                    onChange={selectPeriod}
                    options={[
                      { value: '1', label: t('trafficHistory.day1') },
                      { value: '7', label: t('trafficHistory.day7') },
                      { value: '30', label: t('trafficHistory.day30') },
                      { value: 'custom', label: t('trafficHistory.custom') },
                    ]}
                  />
                  {period === 'custom' && (
                    <>
                      <DatePicker.RangePicker
                        open={pickerOpen}
                        onOpenChange={setPickerOpen}
                        classNames={{ popup: { root: 'traffic-history-picker-popup' } }}
                        aria-label={t('trafficHistory.custom')}
                        showTime={{ format: 'HH:mm' }}
                        format="YYYY-MM-DD HH:mm"
                        value={custom}
                        onChange={(values) =>
                          setCustom(values?.[0] && values[1] ? [values[0], values[1]] : null)
                        }
                        status={invalid ? 'error' : undefined}
                      />
                      <Button type="primary" onClick={applyCustom}>
                        {t('trafficHistory.query')}
                      </Button>
                    </>
                  )}
                </Space>
                {invalid && <Alert type="error" title={t('trafficHistory.invalidRange')} />}
                <Typography.Paragraph type="secondary" className="traffic-history-meta">
                  {t('trafficHistory.timezone')}: {timeZone} · {format(data?.from ?? range.from)} —{' '}
                  {format(data?.to ?? range.to)}
                </Typography.Paragraph>
              </Card>
              <Segmented
                value={view}
                onChange={(value) =>
                  setParams((previous) => {
                    const next = new URLSearchParams(previous);
                    next.set('view', value);
                    return next;
                  })
                }
                options={[
                  { value: 'clients', label: t('domainTraffic.customerTotal') },
                  { value: 'sites', label: t('domainTraffic.title') },
                ]}
              />
              {view === 'sites' ? (
                <DomainTrafficPanel
                  clientId={clientId}
                  from={range.from}
                  to={range.to}
                  timeZone={timeZone}
                  refreshKey={refreshKey}
                />
              ) : (
                <>
                  {(history.error || options.error) && (
                    <Alert
                      type="error"
                      showIcon
                      title={t('somethingWentWrong')}
                      description={(history.error || options.error)?.message}
                    />
                  )}
                  <Row gutter={[16, 16]}>
                    <Col xs={24} md={8}>
                      <Card>
                        <Statistic
                          title={t('trafficHistory.download')}
                          value={trafficBytes(data?.down ?? 0)}
                          prefix={<ArrowDownOutlined aria-hidden />}
                        />
                      </Card>
                    </Col>
                    <Col xs={24} md={8}>
                      <Card>
                        <Statistic
                          title={t('trafficHistory.upload')}
                          value={trafficBytes(data?.up ?? 0)}
                          prefix={<ArrowUpOutlined aria-hidden />}
                        />
                      </Card>
                    </Col>
                    <Col xs={24} md={8}>
                      <Card>
                        <Statistic
                          title={t('trafficHistory.total')}
                          value={trafficBytes((data?.up ?? 0) + (data?.down ?? 0))}
                        />
                      </Card>
                    </Col>
                  </Row>
                  <Card
                    title={t('trafficHistory.chart')}
                    loading={history.isLoading}
                    extra={
                      data && (
                        <Tag>
                          {t('trafficHistory.grain')}: {resolutionLabels[data.resolution]}
                        </Tag>
                      )
                    }
                  >
                    {data?.startedAt ? (
                      <TrafficHistoryChart points={data.points} timeZone={timeZone} />
                    ) : (
                      <Empty description={t('trafficHistory.noHistory')} />
                    )}
                    {missing && <Alert type="info" showIcon title={t('trafficHistory.gaps')} />}
                  </Card>
                  <Card
                    title={t('trafficHistory.detail')}
                    extra={
                      clientId > 0 && (
                        <Button type="link" onClick={() => selectClient(0)}>
                          {t('trafficHistory.allClients')}
                        </Button>
                      )
                    }
                  >
                    <Table
                      rowKey="id"
                      columns={columns}
                      dataSource={data?.clients ?? []}
                      loading={history.isLoading}
                      pagination={{ pageSize: 20, hideOnSinglePage: true }}
                      scroll={{ x: 700 }}
                    />
                  </Card>
                  <Typography.Paragraph type="secondary">
                    {t('trafficHistory.notice')}
                    <br />
                    {t('trafficHistory.firstSample')}:{' '}
                    {format(data?.startedAt ?? options.data?.startedAt ?? 0)} ·{' '}
                    {t('trafficHistory.lastSample')}:{' '}
                    {format(data?.lastSample ?? options.data?.lastSample ?? 0)}
                  </Typography.Paragraph>
                </>
              )}
            </div>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
