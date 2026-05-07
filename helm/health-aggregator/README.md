# Health Aggregator Helm Chart

This Helm chart provides a deployment for the Health Aggregator application, which aggregates health information from 
various sources and exposes it through a REST API. It is designed to be easily configurable and deployable using 
Kubernetes.

## Testing

Testing changes using `helm template`:
~~~bash
helm template health-aggregator ./helm/health-aggregator # --set ingress.enabled=true
~~~

In a running instance, use the following command to test the health aggregator:
~~~shell
wget -q -S -O - http://heath-aggergator-health-aggregator.<namespace>.svc:8080
~~~

## Deployment

or from the Kubectl command line:
~~~bash
NAMESPACE=jenkins
POD=$(kubectl get pods --no-headers -o custom-columns=":metadata.name" -n $NAMESPACE | grep health-aggregator)
kubectl exec -it --namespace=$NAMESPACE $POD -- sh -c "wget -q -S -O - http://heath-aggergator-health-aggregator.$NAMESPACE.svc:8080"
~~~

~~~bash
cd helm/health-aggregator
mkdir -p tmp
helm template . -f .\values.yaml > tmp/helm.yaml 
helm lint . -f .\values.yaml
~~~