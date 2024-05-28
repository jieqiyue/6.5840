<!--

    Licensed to the Apache Software Foundation (ASF) under one
    or more contributor license agreements.  See the NOTICE file
    distributed with this work for additional information
    regarding copyright ownership.  The ASF licenses this file
    to you under the Apache License, Version 2.0 (the
    "License"); you may not use this file except in compliance
    with the License.  You may obtain a copy of the License at
    
        http://www.apache.org/licenses/LICENSE-2.0
    
    Unless required by applicable law or agreed to in writing,
    software distributed under the License is distributed on an
    "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
    KIND, either express or implied.  See the License for the
    specific language governing permissions and limitations
    under the License.

-->
# 简介
6.5840（原6.824）分布式系统。
本项目从0开始，实现了一个分布式k/v存储系统。系统分为Raft层，以及应用层。目前已经实现了Raft层。

# 主要特点
本项目主要特点如下：
Raft部分：
1. 实现了领导人选举。
2. 日志分发，以及论文中提到的安全性校验。
3. SnapShot快照机制，防止Log过大。
4. 快速回退NextIndex。

# 测试情况
目前该项目通过了Lab3的所有测试。
测试数量平均在两千次以上。
![image](https://github.com/jieqiyue/6.5840/assets/53082256/8ab55eec-cf38-4f58-8801-cb3756a9b5a1)
