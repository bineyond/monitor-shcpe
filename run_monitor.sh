#!/bin/bash

# 过滤掉已知的Chrome错误信息
./monitor 2>&1 | grep -v "ERROR: could not unmarshal event: unknown IPAddressSpace value: Loopback"
