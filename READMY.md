# Deploy
```
helm upgrade --install \
  --namespace "default" \
  --create-namespace \
  "booking-queue-manager" \
  "./helm/booking-queue-manager" \
  --set image.repository="dn010590sas/bookingprocessor" \
  --set image.tag="latest"
```