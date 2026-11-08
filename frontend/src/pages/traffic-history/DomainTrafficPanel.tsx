import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Col,
  Empty,
  Input,
  Progress,
  Row,
  Segmented,
  Space,
  Statistic,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import { ArrowDownOutlined, ArrowUpOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import type {
  DomainTrafficControl,
  DomainTrafficRank,
  DomainTrafficResponse,
} from '@/generated/types';
import TrafficHistoryChart, { trafficBytes } from './TrafficHistoryChart';

function serviceName(key: string): string {
  if (key.startsWith('domain:')) return key.slice(7);
  if (key.startsWith('custom:')) return key.slice(7);
  const brands: Record<string, string> = {
    google: 'Google',
    youtube: 'YouTube',
    facebook: 'Facebook',
    github: 'GitHub',
    openai: 'OpenAI',
    apple: 'Apple',
    microsoft: 'Microsoft',
    telegram: 'Telegram',
    netflix: 'Netflix',
    instagram: 'Instagram',
    twitter: 'X / Twitter',
  };
  return brands[key] || key;
}

export default function DomainTrafficPanel({
  clientId,
  from,
  to,
  timeZone,
  refreshKey,
}: {
  clientId: number;
  from: number;
  to: number;
  timeZone: string;
  refreshKey: number;
}) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const [draftEnabled, setDraftEnabled] = useState<boolean | null>(null);
  const [group, setGroup] = useState<'service' | 'domain'>('service');
  const [service, setService] = useState('');
  const [domain, setDomain] = useState('');
  const [search, setSearch] = useState('');
  const [searchText, setSearchText] = useState('');
  const query = useQuery({
    queryKey: ['domainTraffic', clientId, from, to, service, domain, search, refreshKey],
    queryFn: async ({ signal }) => {
      const msg = await HttpUtil.get<DomainTrafficResponse>(
        '/panel/api/clients/domain-traffic',
        { clientId, from, to, service, domain, search },
        { signal, silent: true },
      );
      if (!msg.success || !msg.obj) throw new Error(msg.msg || t('somethingWentWrong'));
      return msg.obj;
    },
    refetchInterval: 15_000,
  });
  const data = query.data;
  const status = data?.status;
  const control = useQuery({
    queryKey: ['domainTrafficControl'],
    queryFn: async ({ signal }) => {
      const msg = await HttpUtil.get<DomainTrafficControl>(
        '/panel/api/clients/domain-traffic/settings',
        {},
        { signal, silent: true },
      );
      if (!msg.success || !msg.obj) throw new Error(msg.msg || t('somethingWentWrong'));
      return msg.obj;
    },
    refetchInterval: 15_000,
  });
  const changeMode = useMutation({
    mutationFn: async (enabled: boolean) => {
      const msg = await HttpUtil.post<DomainTrafficControl>(
        '/panel/api/clients/domain-traffic/settings',
        { enabled },
        { headers: { 'Content-Type': 'application/json' }, silent: true },
      );
      if (msg.obj) queryClient.setQueryData(['domainTrafficControl'], msg.obj);
      if (!msg.success || !msg.obj) throw new Error(msg.msg || t('somethingWentWrong'));
      return msg.obj;
    },
    onSuccess: () => setDraftEnabled(null),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['domainTrafficControl'] });
      void queryClient.invalidateQueries({ queryKey: ['domainTraffic'] });
    },
  });
  const selectedEnabled = draftEnabled ?? control.data?.configured ?? false;
  const collectionWarmingUp =
    changeMode.isPending ||
    (changeMode.isSuccess && query.dataUpdatedAt <= changeMode.submittedAt + 10_000);
  const needsApply =
    !!control.data &&
    (selectedEnabled !== control.data.configured ||
      control.data.configured !== control.data.enabled ||
      !control.data.running ||
      (!!control.data.error && control.data.configured));
  const displayGroup = data?.archived ? 'service' : group;
  const label = (key: string) =>
    key === 'unknown' || key === '__unknown__' ? t('domainTraffic.unknown') : serviceName(key);
  const total = (data?.up ?? 0) + (data?.down ?? 0);
  const columns = [
    {
      title: t(displayGroup === 'service' ? 'domainTraffic.service' : 'domainTraffic.domain'),
      dataIndex: 'key',
      key: 'name',
      render: (key: string) => (
        <Button
          type="link"
          className="traffic-domain-name"
          onClick={() => {
            if (displayGroup === 'service') {
              setService(key);
              setGroup('domain');
            } else setDomain(key);
          }}
        >
          {label(key)}
        </Button>
      ),
    },
    {
      title: t('trafficHistory.download'),
      dataIndex: 'down',
      key: 'down',
      render: trafficBytes,
      sorter: (a: DomainTrafficRank, b: DomainTrafficRank) => a.down - b.down,
    },
    {
      title: t('trafficHistory.upload'),
      dataIndex: 'up',
      key: 'up',
      render: trafficBytes,
      sorter: (a: DomainTrafficRank, b: DomainTrafficRank) => a.up - b.up,
    },
    {
      title: t('trafficHistory.total'),
      key: 'total',
      render: (_: unknown, r: DomainTrafficRank) => trafficBytes(r.up + r.down),
      sorter: (a: DomainTrafficRank, b: DomainTrafficRank) => a.up + a.down - b.up - b.down,
    },
    {
      title: t('domainTraffic.share'),
      key: 'share',
      width: 150,
      render: (_: unknown, r: DomainTrafficRank) => (
        <Progress
          size="small"
          percent={total ? Number((((r.up + r.down) / total) * 100).toFixed(1)) : 0}
        />
      ),
    },
  ];
  const format = (ts: number) =>
    ts
      ? new Intl.DateTimeFormat(i18n.language, {
          timeZone,
          dateStyle: 'medium',
          timeStyle: 'short',
        }).format(new Date(ts * 1000))
      : '—';
  return (
    <div className="traffic-history-stack">
      <Card>
        <Space wrap>
          <Typography.Text>{t('domainTraffic.collection')}</Typography.Text>
          <Switch
            aria-label={t('domainTraffic.collection')}
            checked={selectedEnabled}
            onChange={setDraftEnabled}
            disabled={!control.data || changeMode.isPending}
          />
          <Button
            type="primary"
            loading={changeMode.isPending}
            disabled={!needsApply || control.isLoading}
            onClick={() => changeMode.mutate(selectedEnabled)}
          >
            {t('domainTraffic.saveRestart')}
          </Button>
          {control.data && (
            <Tag color={control.data.enabled ? 'green' : undefined}>
              {t('domainTraffic.runtime')}:{' '}
              {t(
                !control.data.running
                  ? 'domainTraffic.stopped'
                  : !control.data.supported
                    ? 'domainTraffic.unavailable'
                    : control.data.enabled
                      ? 'domainTraffic.collecting'
                      : 'domainTraffic.collectionOff',
              )}
            </Tag>
          )}
          {control.data && control.data.configured !== control.data.enabled && (
            <Tag color="warning">{t('domainTraffic.pending')}</Tag>
          )}
        </Space>
        <Typography.Paragraph type="secondary" className="traffic-history-meta">
          {t('domainTraffic.restartNotice')}
        </Typography.Paragraph>
      </Card>
      {(control.error || changeMode.error) && (
        <Alert
          type="error"
          showIcon
          title={t('somethingWentWrong')}
          description={(changeMode.error || control.error)?.message}
        />
      )}
      {control.data?.running && control.data.supported && !control.data.enabled && (
        <Alert
          type="info"
          showIcon
          title={t('domainTraffic.collectionOff')}
          description={t('domainTraffic.historyAvailable')}
        />
      )}
      {control.data?.enabled && collectionWarmingUp && !status?.enabled && (
        <Alert type="info" showIcon title={t('domainTraffic.waiting')} />
      )}
      <Card>
        <Space wrap>
          <Segmented<'service' | 'domain'>
            value={displayGroup}
            onChange={setGroup}
            options={[
              { value: 'service', label: t('domainTraffic.services') },
              { value: 'domain', label: t('domainTraffic.domains'), disabled: !!data?.archived },
            ]}
          />
          <Input.Search
            aria-label={t('domainTraffic.search')}
            placeholder={t('domainTraffic.search')}
            maxLength={128}
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
            allowClear
            onSearch={setSearch}
            className="traffic-domain-search"
          />
          {(service || domain || search) && (
            <Button
              onClick={() => {
                setService('');
                setDomain('');
                setSearch('');
                setSearchText('');
              }}
            >
              {t('domainTraffic.showAll')}
            </Button>
          )}
          {service && (
            <Tag
              closable
              onClose={() => {
                setService('');
                setDomain('');
              }}
            >
              {label(service)}
            </Tag>
          )}
          {domain && (
            <Tag closable onClose={() => setDomain('')}>
              {label(domain)}
            </Tag>
          )}
        </Space>
      </Card>
      {query.error && (
        <Alert
          type="error"
          showIcon
          title={t('somethingWentWrong')}
          description={query.error.message}
        />
      )}
      {((control.data && (!control.data.running || !control.data.supported)) ||
        (control.data?.enabled &&
          !collectionWarmingUp &&
          status &&
          (!status.enabled || query.dataUpdatedAt / 1000 - status.lastSample > 90))) && (
        <Alert
          type="warning"
          showIcon
          title={t('domainTraffic.unavailable')}
          description={control.data?.error || status?.error || t('domainTraffic.waiting')}
        />
      )}
      {status?.ruleError && <Alert type="info" showIcon title={t('domainTraffic.noRules')} />}
      {!!status?.overflow && (
        <Alert
          type="warning"
          showIcon
          title={t('domainTraffic.overflow')}
          description={`${status.overflow} · ${trafficBytes(status.droppedBytes)}`}
        />
      )}
      {data?.archived && (
        <Alert
          type="info"
          showIcon
          title={t('domainTraffic.archived')}
          description={`${format(data.from)} — ${format(data.to)}`}
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
            <Statistic title={t('trafficHistory.total')} value={trafficBytes(total)} />
          </Card>
        </Col>
      </Row>
      <Card
        title={t('trafficHistory.chart')}
        loading={query.isLoading}
        extra={
          data && (
            <Tag>
              {t('trafficHistory.grain')}: {t(`trafficHistory.${data.resolution}`)}
            </Tag>
          )
        }
      >
        {status?.startedAt && data ? (
          <TrafficHistoryChart points={data.points} timeZone={timeZone} />
        ) : (
          <Empty description={t('domainTraffic.waiting')} />
        )}
        {data?.points.some((p) => p.coveredMinutes < p.expectedMinutes) && (
          <Alert type="info" showIcon title={t('trafficHistory.gaps')} />
        )}
      </Card>
      <Card
        title={t('domainTraffic.ranking')}
        extra={
          <Typography.Text type="secondary">
            {t('domainTraffic.top100')} ·{' '}
            {displayGroup === 'service' ? (data?.serviceCount ?? 0) : (data?.domainCount ?? 0)}
          </Typography.Text>
        }
      >
        <Table
          rowKey="key"
          columns={columns}
          dataSource={(displayGroup === 'service' ? data?.services : data?.domains) ?? []}
          loading={query.isLoading}
          pagination={{ pageSize: 20, hideOnSinglePage: true }}
          scroll={{ x: 850 }}
        />
      </Card>
      <Typography.Paragraph type="secondary">
        {t('domainTraffic.notice')}
        <br />
        {t('domainTraffic.retention', {
          detail: status?.detailDays ?? 30,
          summary: status?.summaryDays ?? 365,
        })}
        <br />
        {t('domainTraffic.ruleCount', {
          services: status?.ruleServices ?? 0,
          rules: status?.ruleDomains ?? 0,
        })}{' '}
        · {t('domainTraffic.active')}: {status?.active ?? 0}
        <br />
        {t('trafficHistory.firstSample')}: {format(status?.startedAt ?? 0)} ·{' '}
        {t('trafficHistory.lastSample')}: {format(status?.lastSample ?? 0)}
      </Typography.Paragraph>
    </div>
  );
}
