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

import React, { useEffect, useState, useRef } from 'react';
import {
  Banner,
  Button,
  Form,
  Row,
  Col,
  Typography,
  Spin,
} from '@douyinfe/semi-ui';
const { Text } = Typography;
import {
  API,
  removeTrailingSlash,
  showError,
  showSuccess,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function SettingsPaymentGatewayWechatPay(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    WechatPayNativeEnabled: false,
    WechatPayMchId: '',
    WechatPayAppId: '',
    WechatPayApiV3Key: '',
    WechatPayMchCertSerial: '',
    WechatPayMchPrivateKey: '',
    WechatPayMinTopUp: 0,
  });
  const [originInputs, setOriginInputs] = useState({});
  const formApiRef = useRef(null);

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        WechatPayNativeEnabled:
          props.options.WechatPayNativeEnabled === 'true' ||
          props.options.WechatPayNativeEnabled === true,
        WechatPayMchId: props.options.WechatPayMchId || '',
        WechatPayAppId: props.options.WechatPayAppId || '',
        WechatPayApiV3Key: '',
        WechatPayMchCertSerial: props.options.WechatPayMchCertSerial || '',
        WechatPayMchPrivateKey: '',
        WechatPayMinTopUp:
          props.options.WechatPayMinTopUp !== undefined &&
          props.options.WechatPayMinTopUp !== ''
            ? parseInt(props.options.WechatPayMinTopUp, 10) || 0
            : 0,
      };
      setInputs(currentInputs);
      setOriginInputs({ ...currentInputs });
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const handleFormChange = (values) => {
    setInputs(values);
  };

  const submitWechatPay = async () => {
    if (props.options.ServerAddress === '') {
      showError(t('请先填写服务器地址'));
      return;
    }
    if (inputs.WechatPayApiV3Key && inputs.WechatPayApiV3Key.length !== 32) {
      showError(t('APIv3 密钥须为 32 位'));
      return;
    }

    setLoading(true);
    try {
      const options = [];

      if (
        originInputs['WechatPayNativeEnabled'] !== inputs.WechatPayNativeEnabled
      ) {
        options.push({
          key: 'WechatPayNativeEnabled',
          value: inputs.WechatPayNativeEnabled ? 'true' : 'false',
        });
      }
      if (inputs.WechatPayMchId !== (originInputs.WechatPayMchId || '')) {
        options.push({ key: 'WechatPayMchId', value: inputs.WechatPayMchId });
      }
      if (inputs.WechatPayAppId !== (originInputs.WechatPayAppId || '')) {
        options.push({ key: 'WechatPayAppId', value: inputs.WechatPayAppId });
      }
      if (inputs.WechatPayApiV3Key !== '') {
        options.push({
          key: 'WechatPayApiV3Key',
          value: inputs.WechatPayApiV3Key,
        });
      }
      if (
        inputs.WechatPayMchCertSerial !==
        (originInputs.WechatPayMchCertSerial || '')
      ) {
        options.push({
          key: 'WechatPayMchCertSerial',
          value: inputs.WechatPayMchCertSerial,
        });
      }
      if (inputs.WechatPayMchPrivateKey !== '') {
        options.push({
          key: 'WechatPayMchPrivateKey',
          value: inputs.WechatPayMchPrivateKey,
        });
      }
      if (
        String(inputs.WechatPayMinTopUp ?? 0) !==
        String(originInputs.WechatPayMinTopUp ?? 0)
      ) {
        options.push({
          key: 'WechatPayMinTopUp',
          value: String(inputs.WechatPayMinTopUp ?? 0),
        });
      }

      if (options.length === 0) {
        showSuccess(t('没有需要保存的变更'));
        setLoading(false);
        return;
      }

      const requestQueue = options.map((opt) =>
        API.put('/api/option/', {
          key: opt.key,
          value: opt.value,
        }),
      );
      const results = await Promise.all(requestQueue);
      const errorResults = results.filter((res) => !res.data.success);
      if (errorResults.length > 0) {
        errorResults.forEach((res) => {
          showError(res.data.message);
        });
      } else {
        showSuccess(t('更新成功'));
        props.refresh();
      }
    } catch (e) {
      showError(t('保存失败'));
    } finally {
      setLoading(false);
    }
  };

  const callbackBase = removeTrailingSlash(
    props.options.CustomCallbackAddress || props.options.ServerAddress || '',
  );
  const notifyExample =
    callbackBase !== '' ? `${callbackBase}/api/wechatpay/notify` : '';

  return (
    <Spin spinning={loading}>
      <div style={{ padding: '12px 0' }}>
        <Text strong style={{ fontSize: '16px' }}>
          {t('微信支付（Native 直连）')}
        </Text>
        <Text type='secondary' style={{ marginLeft: 8 }}>
          CNY · APIv3
        </Text>
      </div>
      {notifyExample && (
        <Banner
          type='info'
          description={
            <span>
              {t('请在微信商户平台将支付结果通知 URL 配置为：')}
              <Text code copyable>
                {notifyExample}
              </Text>
            </span>
          }
          style={{ marginBottom: 16 }}
        />
      )}
      <Form
        getFormApi={(api) => (formApiRef.current = api)}
        onValueChange={handleFormChange}
      >
        <Row gutter={16}>
          <Col span={24}>
            <Form.Switch
              field='WechatPayNativeEnabled'
              label={t('启用微信 Native 充值')}
            />
          </Col>
          <Col xs={24} md={12}>
            <Form.Input
              field='WechatPayMchId'
              label={t('商户号 mchid')}
              placeholder='190000xxxx'
            />
          </Col>
          <Col xs={24} md={12}>
            <Form.Input
              field='WechatPayAppId'
              label={t('AppID')}
              placeholder='wx........'
            />
          </Col>
          <Col xs={24} md={12}>
            <Form.Input
              field='WechatPayMchCertSerial'
              label={t('商户 API 证书序列号')}
            />
          </Col>
          <Col xs={24} md={12}>
            <Form.Input
              field='WechatPayApiV3Key'
              label={t('APIv3 密钥（32 位，留空则不修改）')}
              mode='password'
            />
          </Col>
          <Col span={24}>
            <Form.TextArea
              field='WechatPayMchPrivateKey'
              label={t('商户 API 私钥 PEM（留空则不修改）')}
              rows={6}
              placeholder='-----BEGIN PRIVATE KEY-----'
            />
          </Col>
          <Col xs={24} md={12}>
            <Form.InputNumber
              field='WechatPayMinTopUp'
              label={t('最小充值（与运营单位一致，0 表示沿用全局最小充值）')}
              min={0}
              max={1000000}
            />
          </Col>
        </Row>
        <Button type='primary' onClick={submitWechatPay} style={{ marginTop: 16 }}>
          {t('保存微信 Native 设置')}
        </Button>
      </Form>
    </Spin>
  );
}
