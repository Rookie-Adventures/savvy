/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Form,
  Row,
  Col,
  Spin,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess, toBoolean } from '../../../helpers';
import { useTranslation } from 'react-i18next';
import { BookOpen, TriangleAlert } from 'lucide-react';

// ponytail: classic admin 支付宝直连配置块。
// 镜像 def-web Task8b payment-settings-section.tsx Alipay tab 的字段集(15 key 中的 9 个支付宝 key)。
// 公钥模式 = AppId + AppPrivateKey + PublicKey;证书模式 = AppId + AppPrivateKey + 3 个 PEM 证书内容。
// AlipayIsCertMode Switch 切换两套字段显隐。AlipayIsProduction 控沙箱 vs 正式网关。
// SECRET(AppPrivateKey) 留空表示保持当前不变(照 StripeApiSecret 模式)。
export default function SettingsPaymentGatewayAlipay(props) {
  const { t } = useTranslation();
  const sectionTitle = props.hideSectionTitle ? undefined : t('支付宝设置');
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    AlipayAppId: '',
    AlipayAppPrivateKey: '',
    AlipayPublicKey: '',
    AlipayIsCertMode: false,
    AlipayIsProduction: false,
    AlipayAppCertSN: '',
    AlipayAlipayCertSN: '',
    AlipayRootCertSN: '',
    AlipayNotifyURL: '',
  });
  const [originInputs, setOriginInputs] = useState({});
  const formApiRef = useRef(null);

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        AlipayAppId: props.options.AlipayAppId || '',
        AlipayAppPrivateKey: '',
        AlipayPublicKey: props.options.AlipayPublicKey || '',
        AlipayIsCertMode: toBoolean(props.options.AlipayIsCertMode),
        AlipayIsProduction: toBoolean(props.options.AlipayIsProduction),
        AlipayAppCertSN: props.options.AlipayAppCertSN || '',
        AlipayAlipayCertSN: props.options.AlipayAlipayCertSN || '',
        AlipayRootCertSN: props.options.AlipayRootCertSN || '',
        AlipayNotifyURL: props.options.AlipayNotifyURL || '',
      };
      setInputs(currentInputs);
      setOriginInputs({ ...currentInputs });
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const handleFormChange = (values) => {
    setInputs(values);
  };

  const submitAlipaySetting = async () => {
    setLoading(true);
    try {
      const options = [];
      const pushIfChanged = (key, value) => {
        if (originInputs[key] !== value) {
          options.push({ key, value });
        }
      };
      pushIfChanged('AlipayAppId', inputs.AlipayAppId || '');
      // ponytail: SECRET 留空不提交(保持当前不变),照 StripeApiSecret 模式。
      if (inputs.AlipayAppPrivateKey && inputs.AlipayAppPrivateKey !== '') {
        options.push({ key: 'AlipayAppPrivateKey', value: inputs.AlipayAppPrivateKey });
      }
      pushIfChanged('AlipayPublicKey', inputs.AlipayPublicKey || '');
      pushIfChanged('AlipayIsCertMode', inputs.AlipayIsCertMode ? 'true' : 'false');
      pushIfChanged('AlipayIsProduction', inputs.AlipayIsProduction ? 'true' : 'false');
      pushIfChanged('AlipayAppCertSN', inputs.AlipayAppCertSN || '');
      pushIfChanged('AlipayAlipayCertSN', inputs.AlipayAlipayCertSN || '');
      pushIfChanged('AlipayRootCertSN', inputs.AlipayRootCertSN || '');
      pushIfChanged('AlipayNotifyURL', inputs.AlipayNotifyURL || '');

      if (options.length === 0) {
        showSuccess(t('无变更'));
        setLoading(false);
        return;
      }

      const requestQueue = options.map((opt) =>
        API.put('/api/option/', { key: opt.key, value: opt.value }),
      );
      const results = await Promise.all(requestQueue);
      const errorResults = results.filter((res) => !res.data.success);
      if (errorResults.length > 0) {
        errorResults.forEach((res) => showError(res.data.message));
      } else {
        showSuccess(t('更新成功'));
        setOriginInputs({ ...inputs });
        props.refresh?.();
      }
    } catch (error) {
      showError(t('更新失败'));
    }
    setLoading(false);
  };

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={handleFormChange}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={sectionTitle}>
          <Banner
            type='info'
            icon={<BookOpen size={16} />}
            description={
              <>
                {t('支付宝官方直连配置。公钥模式填 AppId + 应用私钥 + 支付宝公钥;证书模式开启后填 3 个 PEM 证书内容。')}
                <br />
                {t('回调地址')}：
                {props.options.ServerAddress
                  ? props.options.ServerAddress
                  : t('网站地址')}
                /api/user/alipay/notify
              </>
            }
            style={{ marginBottom: 12 }}
          />
          <Banner
            type='warning'
            icon={<TriangleAlert size={16} />}
            description={t('应用私钥留空表示保持当前不变(不回显)。关闭「正式环境」开关使用沙箱网关。')}
            style={{ marginBottom: 16 }}
          />
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayAppId'
                label={t('App ID')}
                placeholder={t('例如：2021000...')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayNotifyURL'
                label={t('异步通知 URL')}
                placeholder={t('留空则用服务器地址 + /api/user/alipay/notify')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Switch
                field='AlipayIsProduction'
                size='default'
                checkedText='｜'
                uncheckedText='〇'
                label={t('正式环境')}
                extraText={t('开启=正式网关,关闭=沙箱网关')}
              />
            </Col>
          </Row>
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }} style={{ marginTop: 16 }}>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.Switch
                field='AlipayIsCertMode'
                size='default'
                checkedText='｜'
                uncheckedText='〇'
                label={t('证书模式')}
                extraText={t('公钥模式=应用私钥+支付宝公钥;证书模式=3 个 PEM 证书内容')}
              />
            </Col>
          </Row>
          {inputs.AlipayIsCertMode ? (
            <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }} style={{ marginTop: 16 }}>
              <Col xs={24} sm={24} md={8} lg={8} xl={8}>
                <Form.TextArea
                  field='AlipayAppCertSN'
                  label={t('应用公钥证书 PEM 内容')}
                  placeholder={'-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----'}
                  rows={4}
                />
              </Col>
              <Col xs={24} sm={24} md={8} lg={8} xl={8}>
                <Form.TextArea
                  field='AlipayAlipayCertSN'
                  label={t('支付宝公钥证书 PEM 内容')}
                  placeholder={'-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----'}
                  rows={4}
                />
              </Col>
              <Col xs={24} sm={24} md={8} lg={8} xl={8}>
                <Form.TextArea
                  field='AlipayRootCertSN'
                  label={t('支付宝根证书 PEM 内容')}
                  placeholder={'-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----'}
                  rows={4}
                />
              </Col>
            </Row>
          ) : (
            <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }} style={{ marginTop: 16 }}>
              <Col xs={24} sm={24} md={12} lg={12} xl={12}>
                <Form.Input
                  field='AlipayAppPrivateKey'
                  label={t('应用私钥')}
                  placeholder={t('留空表示保持当前不变')}
                  type='password'
                />
              </Col>
              <Col xs={24} sm={24} md={12} lg={12} xl={12}>
                <Form.TextArea
                  field='AlipayPublicKey'
                  label={t('支付宝公钥')}
                  placeholder={t('支付宝公钥 PEM')}
                  rows={4}
                />
              </Col>
            </Row>
          )}
          <Button onClick={submitAlipaySetting} style={{ marginTop: 16 }}>
            {t('更新支付宝设置')}
          </Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
