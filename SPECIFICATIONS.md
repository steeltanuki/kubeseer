# Kubeseer — Specifiche funzionali e suddivisione Walden

## 1. Scopo del documento

Questo documento descrive l'architettura funzionale di Kubeseer e suddivide il prodotto in feature indipendenti, verificabili e approvabili tramite Walden.

Ogni feature elencata deve essere implementata come una directory Walden separata:

```text
.walden/specs/<feature>/
  requirements.md
  design.md
  tasks.md
```

Le feature devono usare criteri di accettazione EARS con identificatori stabili, copertura completa nel design e prove eseguibili nei task.

## 2. Visione generale

Kubeseer è un operatore Kubernetes che consente di:

- osservare risorse Kubernetes built-in e custom;
- selezionare risorse provenienti da uno o più namespace;
- estrarre valori tramite JSONPath;
- convertire i valori in output tipizzati;
- filtrare e aggregare i risultati;
- pubblicare il risultato nello status della Custom Resource Kubeseer;
- limitare le risorse e i namespace osservabili attraverso una policy definita dall'amministratore durante l'installazione.

La configurazione di una singola risorsa Kubeseer può restringere il perimetro consentito dall'installazione, ma non può ampliarlo.

## 3. Decisioni architetturali iniziali

- Il meccanismo iniziale di estrazione è JSONPath.
- CEL è esplicitamente fuori scope per la prima versione e potrà essere aggiunto in seguito.
- Gli operatori disponibili formano un insieme controllato e validato.
- I risultati mantengono il tipo logico del dato.
- Kubeseer supporta l'aggregazione di informazioni provenienti da namespace diversi.
- L'amministratore definisce durante l'installazione namespace e tipi di risorse osservabili.
- Gli utenti che creano risorse Kubeseer non devono possedere direttamente permessi di lettura sulle risorse osservate.
- L'operatore non deve effettuare letture al di fuori del perimetro definito dalla policy di installazione.
- Gli aggiornamenti dello status devono avvenire soltanto quando il risultato cambia semanticamente.

## 4. Contesto stabile Walden

Le informazioni trasversali e stabili del progetto devono essere riportate in `.walden/constitution.md`.

Il file dovrebbe includere almeno:

- scopo di Kubeseer;
- glossario dei termini;
- stack tecnologico;
- versione minima di Kubernetes;
- convenzioni Go e controller-runtime;
- convenzioni delle API Kubernetes;
- comandi standard di build, test e lint;
- decisioni architetturali riportate nella sezione precedente;
- regole di sicurezza non derogabili.

## 5. Struttura delle feature Walden

```text
.walden/
  constitution.md
  environment.md
  specs/
    kubeseer-api-foundation/
    resource-discovery/
    installation-access-policy/
    resource-selection/
    field-extraction/
    typed-output-model/
    value-operators/
    cross-namespace-aggregation/
    reconciliation-runtime/
    status-and-conditions/
    authorization-enforcement/
    admission-validation/
    observability/
    performance-and-limits/
    packaging-and-installation/
    end-to-end-scenarios/
```

---

# 6. Specifiche separate

## 6.1 `kubeseer-api-foundation`

### Obiettivo

Definire il contratto fondamentale della Custom Resource Kubeseer senza introdurre ancora la logica di osservazione o aggregazione.

### Include

- API group, version e kind;
- struttura generale di `spec`;
- struttura generale di `status`;
- naming e identificatori;
- strategia di versionamento dell'API;
- campi obbligatori e opzionali;
- defaulting;
- compatibilità futura;
- `observedGeneration`;
- eventuali riferimenti ad altre risorse di configurazione.

### Non include

- discovery delle risorse;
- JSONPath;
- autorizzazioni;
- operatori;
- aggregazioni;
- comportamento del reconciler.

### Risultato verificabile

- la CRD viene installata correttamente;
- Kubernetes accetta manifest Kubeseer validi;
- Kubernetes rifiuta manifest strutturalmente invalidi;
- le API Go generate compilano e superano i test.

### Dipendenze

Nessuna.

---

## 6.2 `resource-discovery`

### Obiettivo

Risolvere dinamicamente i tipi di risorsa Kubernetes richiesti dalle sorgenti Kubeseer.

### Include

- risoluzione di `apiVersion` e `kind` nel relativo `GroupVersionResource`;
- utilizzo del discovery client Kubernetes;
- supporto a risorse built-in;
- supporto a Custom Resource Definition;
- distinzione tra risorse namespaced e cluster-scoped;
- gestione delle API non disponibili;
- invalidazione o aggiornamento della discovery cache;
- gestione della rimozione o modifica di una CRD.

### Comportamenti di errore

- un tipo inesistente deve produrre un errore associato alla singola sorgente;
- il fallimento di una sorgente non deve interrompere necessariamente le altre sorgenti;
- una risorsa cluster-scoped non deve essere trattata come namespaced;
- gli errori di discovery devono essere esposti nello status in forma comprensibile.

### Dipendenze

- `kubeseer-api-foundation`.

---

## 6.3 `installation-access-policy`

### Obiettivo

Permettere all'amministratore di definire, durante l'installazione, il perimetro massimo di osservazione dell'operatore.

### Include

- namespace accessibili;
- lista esplicita di namespace;
- modalità tutti i namespace;
- modalità tutti i namespace non di sistema;
- inclusioni ed esclusioni esplicite;
- identificazione configurabile dei namespace di sistema;
- API group e kind consentiti;
- risorse built-in e custom consentite;
- accesso opzionale alle risorse cluster-scoped;
- validazione della policy;
- comportamento in caso di policy assente o invalida;
- rapporto tra policy logica e RBAC Kubernetes effettivi.

### Modello indicativo

```yaml
apiVersion: kubeseer.io/v1alpha1
kind: KubeseerAccessPolicy
metadata:
  name: default
spec:
  namespaces:
    mode: AllNonSystem
    include: []
    exclude:
      - kube-system
      - kube-public
      - kube-node-lease
  resources:
    - apiGroups: [""]
      kinds: ["Pod", "Service", "ConfigMap"]
    - apiGroups: ["apps"]
      kinds: ["Deployment", "StatefulSet"]
    - apiGroups: ["example.io"]
      kinds: ["MyCustomResource"]
```

### Regole fondamentali

- la policy costituisce il limite massimo dell'installazione;
- una Kubeseer può restringere il perimetro, ma non ampliarlo;
- una richiesta fuori policy deve essere respinta prima di leggere la risorsa;
- la configurazione amministrativa deve essere distinta dalla configurazione delle singole Kubeseer.

### Dipendenze

- `kubeseer-api-foundation`;
- `resource-discovery`.

---

## 6.4 `resource-selection`

### Obiettivo

Definire come una sorgente Kubeseer seleziona le istanze concrete delle risorse da osservare.

### Include

- selezione per nome;
- selezione per namespace singolo;
- selezione mediante lista di namespace;
- selezione mediante label selector;
- eventuale field selector;
- selezione di tutte le risorse corrispondenti;
- combinazione delle regole di selezione;
- ordinamento deterministico dei risultati;
- comportamento quando nessuna risorsa corrisponde;
- identificatore stabile della sorgente.

### Modello indicativo

```yaml
sources:
  - id: frontend-pods
    resource:
      apiVersion: v1
      kind: Pod
    namespaces:
      names:
        - frontend
        - shared-services
    selector:
      matchLabels:
        app: frontend
```

### Non include

- estrazione JSONPath;
- conversione tipizzata;
- aggregazione;
- enforcement della policy amministrativa.

### Dipendenze

- `resource-discovery`;
- `installation-access-policy`.

---

## 6.5 `field-extraction`

### Obiettivo

Estrarre valori dalle risorse selezionate tramite un sottoinsieme dichiarato e validato di JSONPath.

### Include

- sintassi JSONPath supportata;
- compilazione e validazione delle espressioni;
- estrazione di valori scalari;
- estrazione di liste;
- accesso ad array e mappe;
- risultati multipli;
- campo mancante;
- valore nullo;
- errore di sintassi;
- errore di valutazione;
- eventuale caching delle espressioni compilate;
- limiti intenzionali rispetto all'implementazione completa di JSONPath.

### Modello indicativo

```yaml
fields:
  - name: phase
    path: "{.status.phase}"
  - name: node
    path: "{.spec.nodeName}"
```

### Regole fondamentali

- il valore Kubernetes nativo deve essere preservato fino alla conversione tipizzata;
- un JSONPath invalido deve essere rilevato prima dell'elaborazione delle risorse;
- un errore su un campo deve essere attribuito al campo e alla risorsa corretti;
- deve essere definita esplicitamente la semantica dei risultati multipli.

### Dipendenze

- `resource-selection`.

---

## 6.6 `typed-output-model`

### Obiettivo

Definire un modello di output esplicitamente tipizzato e serializzabile nello status Kubernetes.

### Tipi iniziali

- `string`;
- `integer`;
- `number`;
- `boolean`;
- `timestamp`;
- `duration`;
- `quantity`;
- `object`;
- `list`.

### Include

- rappresentazione interna dei tipi;
- rappresentazione YAML e JSON;
- conversioni ammesse;
- conversioni vietate;
- gestione degli overflow;
- gestione degli errori di conversione;
- conservazione opzionale del valore originale;
- normalizzazione di duration e quantity;
- distinzione tra valore assente, nullo, vuoto e non convertibile;
- rappresentazione degli errori per singolo campo.

### Modello indicativo

```yaml
fields:
  - name: availableReplicas
    path: "{.status.availableReplicas}"
    type: integer
  - name: memoryLimit
    path: "{.spec.template.spec.containers[0].resources.limits.memory}"
    type: quantity
```

```yaml
status:
  result:
    fields:
      availableReplicas:
        type: integer
        value: 3
      memoryLimit:
        type: quantity
        value: 512Mi
        normalizedValue: 536870912
```

### Dipendenze

- `field-extraction`.

---

## 6.7 `value-operators`

### Obiettivo

Definire un insieme controllato di operatori applicabili ai valori estratti e tipizzati.

### Operatori iniziali suggeriti

#### Confronto

- `eq`;
- `ne`;
- `gt`;
- `gte`;
- `lt`;
- `lte`.

#### Stringa

- `contains`;
- `startsWith`;
- `endsWith`;
- `matches`.

#### Presenza

- `exists`;
- `notExists`.

#### Collezioni

- `in`;
- `notIn`.

#### Trasformazioni semplici

- `default`;
- `coalesce`.

### Include

- matrice di compatibilità operatore-tipo;
- semantica dei confronti;
- confronto di quantity e duration;
- comportamento con valori nulli o assenti;
- coercizioni ammesse o vietate;
- ordine di applicazione;
- messaggi di errore deterministici;
- validazione anticipata delle combinazioni non valide.

### Non include

- funzioni di aggregazione;
- linguaggio CEL;
- trasformazioni arbitrarie;
- scripting.

### Dipendenze

- `field-extraction`;
- `typed-output-model`.

---

## 6.8 `cross-namespace-aggregation`

### Obiettivo

Raggruppare e aggregare valori provenienti da più risorse e namespace mantenendone la provenienza.

### Include

- raccolta cross-namespace;
- metadati di provenienza;
- raggruppamento per uno o più campi;
- aggregazioni tipizzate;
- risultati parziali;
- deduplicazione;
- collisioni tra nomi uguali in namespace diversi;
- ordine deterministico;
- cardinalità massima;
- gestione di namespace temporaneamente non disponibili;
- comportamento in presenza di sorgenti degradate.

### Provenienza minima del valore

```yaml
source:
  apiVersion: apps/v1
  kind: Deployment
  namespace: team-a
  name: frontend
  uid: 9d8...
```

### Aggregazioni iniziali suggerite

- `collect`;
- `count`;
- `sum`;
- `min`;
- `max`;
- `average`;
- `first`;
- `last`;
- `distinct`.

### Regole fondamentali

- le aggregazioni numeriche devono accettare soltanto tipi compatibili;
- l'ordine di elaborazione non deve rendere il risultato non deterministico;
- deve essere sempre possibile risalire alle risorse che hanno contribuito al risultato, almeno quando richiesto dalla configurazione;
- il fallimento di una sorgente deve produrre uno stato degradato o un errore secondo una policy esplicita.

### Dipendenze

- `resource-selection`;
- `field-extraction`;
- `typed-output-model`;
- `value-operators`.

---

## 6.9 `reconciliation-runtime`

### Obiettivo

Definire quando e come il controller riconcilia una risorsa Kubeseer.

### Include

- watch delle Custom Resource Kubeseer;
- watch delle risorse sorgente;
- mapping tra risorsa modificata e Kubeseer interessate;
- riconciliazione periodica di sicurezza;
- idempotenza;
- retry e backoff;
- debounce o coalescenza degli eventi;
- gestione degli aggiornamenti concorrenti;
- cancellazione;
- finalizer soltanto se necessario;
- cache e informer;
- invalidazione del risultato;
- confronto semantico prima dell'aggiornamento dello status.

### Regole fondamentali

- una modifica a una risorsa osservata deve riconciliare le Kubeseer interessate;
- una modifica che non cambia il risultato non deve causare un aggiornamento inutile dello status;
- il controller non deve entrare in loop a causa dei propri aggiornamenti di status;
- la riconciliazione deve essere ripetibile e idempotente;
- il fallimento di una Kubeseer non deve bloccare la riconciliazione delle altre.

### Dipendenze

- tutte le feature funzionali necessarie alla prima slice verticale.

---

## 6.10 `status-and-conditions`

### Obiettivo

Definire il contratto osservabile dello status di Kubeseer.

### Include

- `observedGeneration`;
- condizioni Kubernetes;
- risultato tipizzato;
- riepilogo delle sorgenti;
- errori parziali;
- timestamp rilevanti;
- hash o fingerprint del risultato;
- numero di risorse elaborate;
- stato complessivo;
- ragioni e messaggi stabili.

### Condizioni suggerite

- `Accepted`;
- `Authorized`;
- `SourcesResolved`;
- `Ready`;
- `Degraded`.

### Stati da distinguere

- configurazione accettata;
- pronta;
- degradata;
- invalida;
- non autorizzata;
- sorgente non disponibile;
- limite superato.

### Modello indicativo

```yaml
status:
  observedGeneration: 4
  conditions:
    - type: Ready
      status: "True"
      reason: EvaluationSucceeded
      message: Evaluated 12 resources from 3 namespaces
  summary:
    matchedResources: 12
    successfulSources: 3
    failedSources: 0
  result: {}
```

### Dipendenze

- `typed-output-model`;
- `cross-namespace-aggregation`;
- `reconciliation-runtime`.

---

## 6.11 `authorization-enforcement`

### Obiettivo

Applicare a runtime la policy amministrativa e impedire che una Kubeseer ampli il perimetro autorizzato.

### Include

- verifica preventiva dei namespace;
- verifica preventiva di API group e kind;
- controllo delle risorse cluster-scoped;
- prevenzione della privilege escalation;
- comportamento quando la policy viene ristretta;
- invalidazione o rimozione dei risultati precedentemente autorizzati;
- gestione di policy aggiornate durante il runtime;
- audit delle decisioni;
- errori che non espongano dati non autorizzati.

### Regole fondamentali

- una sorgente non autorizzata non deve essere letta;
- l'errore deve essere visibile senza rivelare il contenuto della risorsa vietata;
- una policy più restrittiva deve avere effetto anche sulle Kubeseer esistenti;
- il risultato precedente non deve rimanere esposto come se fosse ancora valido;
- l'identità dell'utente che crea la CR non deve ampliare i privilegi dell'operatore.

### Dipendenze

- `installation-access-policy`;
- `resource-selection`.

---

## 6.12 `admission-validation`

### Obiettivo

Rilevare il prima possibile configurazioni Kubeseer invalide o non autorizzabili.

### Include

- schema OpenAPI della CRD;
- eventuale validating admission webhook;
- validazione dei nomi e degli identificatori;
- unicità degli ID delle sorgenti e dei campi;
- validazione JSONPath;
- validazione delle combinazioni tipo-operatore;
- validazione delle funzioni di aggregazione;
- validazione dei riferimenti;
- validazione rispetto alla policy amministrativa;
- limiti di cardinalità e dimensione;
- messaggi di errore utili.

### Livelli di validazione

1. CRD/OpenAPI per gli errori strutturali e locali.
2. Webhook o controller per discovery, JSONPath avanzato, autorizzazioni e condizioni dipendenti dal cluster.

### Dipendenze

- tutte le feature che definiscono il modello dichiarativo.

---

## 6.13 `observability`

### Obiettivo

Rendere il comportamento dell'operatore diagnosticabile e misurabile.

### Include

- log strutturati;
- riferimenti a namespace, nome e UID della Kubeseer;
- metriche Prometheus;
- Kubernetes Events;
- livelli di log coerenti;
- protezione dei dati sensibili;
- tracciamento delle cause di riconciliazione;
- eventuale tracing, inizialmente opzionale.

### Metriche iniziali suggerite

- riconciliazioni totali;
- riconciliazioni fallite;
- durata della riconciliazione;
- durata della valutazione;
- risorse lette;
- sorgenti fallite;
- risultati prodotti;
- aggiornamenti status evitati;
- errori di autorizzazione;
- errori JSONPath;
- limiti superati.

### Dipendenze

- `reconciliation-runtime`;
- `status-and-conditions`.

---

## 6.14 `performance-and-limits`

### Obiettivo

Definire limiti operativi espliciti e un comportamento sicuro su cluster grandi o configurazioni costose.

### Include

- numero massimo di sorgenti per Kubeseer;
- numero massimo di namespace;
- numero massimo di risorse elaborate;
- dimensione massima dell'output;
- limiti per liste e aggregazioni;
- timeout;
- concorrenza;
- caching;
- memoria massima ragionevole;
- comportamento su cluster grandi;
- protezione da configurazioni patologiche;
- eventuale paginazione delle list Kubernetes.

### Regole fondamentali

- il superamento di un limite deve produrre un risultato deterministico;
- non devono essere pubblicati status che superino limiti sicuri per l'API server;
- i timeout devono essere espliciti e osservabili;
- il caching non deve compromettere correttezza o isolamento;
- più Kubeseer che condividono sorgenti possono riutilizzare dati soltanto quando semanticamente sicuro.

### Dipendenze

- `reconciliation-runtime`;
- `cross-namespace-aggregation`.

---

## 6.15 `packaging-and-installation`

### Obiettivo

Distribuire Kubeseer in modo ripetibile e configurabile.

### Include

- immagini container;
- manifest dell'operatore;
- Helm chart o Kustomize;
- CRD;
- ServiceAccount;
- ClusterRole e Role;
- ClusterRoleBinding e RoleBinding;
- policy iniziale;
- configurazione dei namespace consentiti;
- configurazione dei tipi consentiti;
- installazione e aggiornamento;
- disinstallazione;
- eventuale webhook e certificati;
- compatibilità delle versioni.

### Ruoli da distinguere

#### Amministratore del cluster

- installa l'operatore;
- configura il ServiceAccount;
- definisce il perimetro massimo;
- applica la policy di accesso;
- installa o aggiorna CRD e webhook.

#### Amministratore di namespace o team applicativo

- crea e modifica Kubeseer nei namespace autorizzati;
- seleziona soltanto namespace e tipi consentiti;
- non modifica la policy globale;
- non necessita necessariamente di accesso diretto alle risorse osservate.

#### ServiceAccount dell'operatore

- legge le risorse autorizzate;
- legge le Kubeseer e la policy;
- aggiorna status ed eventi;
- non modifica le risorse osservate.

### Regole fondamentali

- RBAC effettivo e policy logica devono essere il più possibile allineati;
- la policy logica deve essere applicata anche se il ServiceAccount possiede tecnicamente permessi più ampi;
- l'installazione deve permettere la modalità lista esplicita, tutti i namespace e tutti i namespace non di sistema.

### Dipendenze

- `installation-access-policy`;
- `authorization-enforcement`;
- `admission-validation`.

---

## 6.16 `end-to-end-scenarios`

### Obiettivo

Certificare l'integrazione completa delle feature senza introdurre nuova logica applicativa.

### Scenari minimi

1. Lettura di un Deployment nello stesso namespace.
2. Lettura di Pod da più namespace.
3. Lettura di una Custom Resource.
4. Selezione tramite nome.
5. Selezione tramite label.
6. Estrazione JSONPath scalare.
7. Estrazione JSONPath multipla.
8. Campo mancante.
9. Conversione tipizzata riuscita.
10. Conversione tipizzata fallita.
11. Filtro mediante operatore.
12. Aggregazione numerica.
13. Raccolta distinct.
14. Risultato parzialmente degradato.
15. Namespace vietato.
16. Kind vietato.
17. Risorsa o CRD assente.
18. Modifica di una risorsa sorgente.
19. Modifica che non cambia il risultato.
20. Rimozione di una risorsa sorgente.
21. Restrizione della access policy.
22. Restart dell'operatore.
23. Output troppo grande.
24. Superamento del numero massimo di risorse.
25. Cancellazione della Kubeseer.
26. Più Kubeseer con sorgenti sovrapposte.
27. Risorsa cluster-scoped autorizzata.
28. Risorsa cluster-scoped vietata.

### Strategia di verifica suggerita

```text
kind create cluster
make install
make deploy
kubectl apply -f test/e2e/fixtures
go test ./test/e2e/...
```

### Dipendenze

Tutte le feature precedenti.

---

# 7. Ordine di implementazione

## 7.1 Fase 1 — MVP verticale

1. `kubeseer-api-foundation`;
2. `resource-discovery`;
3. `installation-access-policy`;
4. `resource-selection`;
5. `field-extraction`;
6. `typed-output-model`;
7. `reconciliation-runtime`;
8. `status-and-conditions`;
9. `authorization-enforcement`.

### Risultato della fase

Kubeseer può leggere un campo tipizzato da una risorsa autorizzata e pubblicarlo in uno status stabile.

## 7.2 Fase 2 — Filtri e aggregazione

10. `value-operators`;
11. `cross-namespace-aggregation`;
12. `admission-validation`.

### Risultato della fase

Kubeseer può filtrare, raggruppare e aggregare dati provenienti da namespace diversi.

## 7.3 Fase 3 — Produzione

13. `observability`;
14. `performance-and-limits`;
15. `packaging-and-installation`;
16. `end-to-end-scenarios`.

### Risultato della fase

Kubeseer è distribuibile, diagnosticabile, protetto da limiti e verificabile end-to-end.

---

# 8. Grafo delle dipendenze

```text
kubeseer-api-foundation
├── resource-discovery
│   ├── installation-access-policy
│   │   └── authorization-enforcement
│   └── resource-selection
│       ├── field-extraction
│       │   └── typed-output-model
│       │       ├── value-operators
│       │       └── cross-namespace-aggregation
│       └── authorization-enforcement
│
├── reconciliation-runtime
│   └── status-and-conditions
│
├── admission-validation
├── observability
├── performance-and-limits
└── packaging-and-installation

end-to-end-scenarios
└── dipende da tutte le feature precedenti
```

---

# 9. Regola di granularità Walden

Una feature Walden deve rappresentare una capacità osservabile, approvabile e verificabile dall'esterno.

Sono troppo piccole feature come:

- implementare una singola funzione Go;
- aggiungere una singola struct;
- creare un singolo test.

Questi elementi devono essere task interni a una feature.

Sono troppo grandi feature come:

- implementare Kubeseer;
- completare l'operatore;
- realizzare tutto il sistema di aggregazione e sicurezza in un'unica specifica.

La granularità adottata in questo documento permette di modificare requisiti, design e task di una capacità senza invalidare inutilmente l'intero progetto.

# 10. Prima slice consigliata

La prima implementazione dovrebbe attraversare verticalmente il sistema con il minor numero possibile di capacità:

1. una Kubeseer seleziona una singola risorsa per nome;
2. la risorsa appartiene a un namespace autorizzato;
3. il tipo è consentito dalla policy;
4. un JSONPath estrae un singolo valore;
5. il valore viene convertito in un tipo esplicito;
6. il risultato viene scritto nello status;
7. una modifica della risorsa provoca la riconciliazione;
8. lo status non viene aggiornato quando il risultato non cambia.

Soltanto dopo la certificazione di questa slice devono essere introdotti selettori multipli, operatori e aggregazioni avanzate.
