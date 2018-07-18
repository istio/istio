# SOFAMesh

SOFAMesh 是基于 Istio 改进和扩展而来的 Service Mesh 大规模落地实践方案。在继承 Istio 强大功能和丰富特性的基础上，为满足大规模部署下的性能要求以及应对落地实践中的实际情况，有如下改进：

- 采用 Golang 编写的 [MOSN](https://github.com/alipay/sofa-mosn) 取代 [Envoy](https://github.com/envoyproxy/envoy)
- 合并Mixer到数据平面以解决性能瓶颈
- 增强 Pilot 以实现更灵活的服务发现机制
- 增加对 [SOFA RPC](https://github.com/alipay/sofa-rpc)、Dubbo 的支持

初始版本由蚂蚁金服和阿里大文娱UC事业部携手贡献，期待社区一起来参与后续开发，共建一个开源精品项目。

- [SOFAMesh 文档](http://www.sofastack.tech/sofa-mesh/docs/Home)