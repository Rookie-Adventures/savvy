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
import { API, showError, showSuccess } from '../../../helpers';
import { useTranslation } from 'react-i18next';
import { BookOpen, TriangleAlert } from 'lucide-react';

// ponytail: classic admin 微信直连配置块。
// 镜像 def-web Task8b payment-settings-section.tsx WeChat tab 的字段集(6 个 wechat key)。
// 微信 APIv3:AppId + MchID + MchSerial + APIv3Key + PrivateKeyPEM + PlatformCertPath(可选)。
// SECRET(APIv3Key/PrivateKeyPEM) 留空表示保持当前不变(照 StripeApiSecret 模式)。
export default function SettingsPaymentGatewayWechat(props) {
  const { t } = useTranslation();
  const sectionTitle = props.hideSectionTitle ? undefined : t('微信支付设置');
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    WechatAppId: '',
    WechatMchID: '',
    WechatMchSerial: '',
    WechatAPIv3Key: '',
    WechatPrivateKeyPEM: '',
    WechatPlatformCertPath: '',
  });
  const [originInputs, setOriginInputs] = useState({});
  const formApiRef = useRef(null);

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        WechatAppId: props.options.WechatAppId || '',
        WechatMchID: props.options.WechatMchID || '',
        WechatMchSerial: props.options.WechatMchSerial || '',
        WechatAPIv3Key: '',
        WechatPrivateKeyPEM: '',
        WechatPlatformCertPath: props.options.WechatPlatformCertPath || '',
      };
      setInputs(currentInputs);
      setOriginInputs({ ...currentInputs });
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const handleFormChange = (values) => {
    setInputs(values);
  };

  const submitWechatSetting = async () => {
    setLoading(true);
    try {
      const options = [];
      const pushIfChanged = (key, value) => {
        if (originInputs[key] !== value) {
          options.push({ key, value });
        }
      };
      pushIfChanged('WechatAppId', inputs.WechatAppId || '');
      pushIfChanged('WechatMchID', inputs.WechatMchID || '');
      pushIfChanged('WechatMchSerial', inputs.WechatMchSerial || '');
      // ponytail: 2 个 SECRET 留空不提交(保持当前不变)。
      if (inputs.WechatAPIv3Key && inputs.WechatAPIv3Key !== '') {
        options.push({ key: 'WechatAPIv3Key', value: inputs.WechatAPIv3Key });
      }
      if (inputs.WechatPrivateKeyPEM && inputs.WechatPrivateKeyPEM !== '') {
        options.push({ key: 'WechatPrivateKeyPEM', value: inputs.WechatPrivateKeyPEM });
      }
      pushIfChanged('WechatPlatformCertPath', inputs.WechatPlatformCertPath || '');

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
                {t('微信支付 APIv3 直连配置。需已认证服务号 AppId + 商户号 + 证书三件 + APIv3 密钥。')}
                <br />
                {t('回调地址')}：
                {props.options.ServerAddress
                  ? props.options.ServerAddress
                  : t('网站地址')}
                /api/user/wechat/notify
              </>
            }
            style={{ marginBottom: 12 }}
          />
          <Banner
            type='warning'
            icon={<TriangleAlert size={16} />}
            description={t('APIv3 密钥与商户私钥 PEM 留空表示保持当前不变(不回显)。平台证书路径留空则由 SDK 自动下载。')}
            style={{ marginBottom: 16 }}
          />
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WechatAppId'
                label={t('微信 AppId')}
                placeholder={t('例如：wx...')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WechatMchID'
                label={t('商户号')}
                placeholder={t('例如：1900...')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WechatMchSerial'
                label={t('商户证书序列号')}
                placeholder={t('商户证书序列号')}
              />
            </Col>
          </Row>
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }} style={{ marginTop: 16 }}>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WechatAPIv3Key'
                label={t('APIv3 密钥')}
                placeholder={t('留空表示保持当前不变')}
                type='password'
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WechatPlatformCertPath'
                label={t('平台证书路径')}
                placeholder={t('留空则自动下载')}
              />
            </Col>
          </Row>
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }} style={{ marginTop: 16 }}>
            <Col xs={24} sm={24} md={24} lg={24} xl={24}>
              <Form.TextArea
                field='WechatPrivateKeyPEM'
                label={t('商户 API 私钥 PEM')}
                placeholder={'-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----'}
                rows={6}
              />
            </Col>
          </Row>
          <Button onClick={submitWechatSetting} style={{ marginTop: 16 }}>
            {t('更新微信支付设置')}
          </Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
