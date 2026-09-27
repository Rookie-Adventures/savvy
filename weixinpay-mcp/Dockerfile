# 微信AI支付官方插件的服务器侧宿主:fake-hermes 骨架构建期落地 + supergateway stdio→HTTP 桥。
# ZeroClaw 是 distroless 无 shell/node,官方安装器无法直接进宿主,故独立容器承载(与云端会话 fake-hermes 变通同款,下载/验签全走腾讯官方)。
FROM node:24-slim

# 安装器探测 Hermes 宿主:PATH 里要有 hermes,且需要 Node>=22(基础镜像已满足)
RUN printf '#!/bin/sh\nexit 0\n' > /usr/local/bin/hermes && chmod +x /usr/local/bin/hermes
# 设备身份(device_user_id)由 /etc/machine-id 派生;构建期先给一个非空值让安装器落地成功,
# 运行时 entrypoint 用卷内持久 id 覆盖——否则容器重建即"换设备",绑卡状态全丢
RUN cat /proc/sys/kernel/random/boot_id | tr -d '-' > /etc/machine-id

RUN npx -y @tenpay/weixinpay-ai-installer@1.1.0 install \
 && npm install -g supergateway

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENV WECHATPAY_PAYSIGN_SERVICE_ID=agentpay
EXPOSE 8100
ENTRYPOINT ["/entrypoint.sh"]
