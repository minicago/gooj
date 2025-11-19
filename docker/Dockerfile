FROM gcc:12

# 复制time二进制文件
COPY time /usr/bin/time
RUN chmod +x /usr/bin/time

# 验证
RUN /usr/bin/time --version && \
    echo "Time command installed successfully"

WORKDIR /workspace