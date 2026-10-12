# Достопримечательности Минска из GPX Globus

Разбор выполнен 13 октября 2026 года. Файл предоставлен пользователем:
`globus.by.gpx`, 1 583 точки, дата выгрузки в metadata — 28 декабря 2015 года.
Это исторический источник, а не подтверждение состояния объектов в 2026 году.
Техническое время `1992-01-02` в отдельных точках не считается датой проверки.

Выбраны 33 объекта Минска: 25 новых карточек и 8 совпадений с каталогом.
Итог — 60 карточек. Совпадения установлены вручную по названию, назначению
и координатам; близкие постройки одного комплекса не объединены автоматически.
У восьми существующих мест сохранены координаты подходов, цены, расписания,
описания, slug и дата прежней проверки; добавлена только ссылка на GPX-источник.

В пределах широкого прямоугольника валидатора обнаружены 47 точек.
14 относятся к окрестностям: Сёмково, Сеница, Новый Двор, Озерцо, Колодищи,
Куропаты и Прилуки. Прямоугольник валидации не является границей Минска.
Эти точки не добавлены в городской каталог; решения сохранены в манифесте.
Отбор кабинетный, без вычисления по актуальному административному полигону.

## Происхождение данных

[Манифест](../data/sources/globus-minsk.json) сохраняет SHA-256 оригинального
файла, исходные названия, координаты, описания, времена и решение для каждой
из 47 близлежащих точек. Индексы ниже начинаются с нуля: это порядковые номера
`wpt`, а не идентификаторы сайта. [Выборка GPX](../data/sources/globus-minsk.gpx)
содержит все 33 выбранные точки без исправления их исходных текстов.
Ссылка `https://globus.by/` в карточке обозначает сайт источника; она не выдаётся
за страницу конкретного объекта. Конкретная запись находится по индексу
в сохранённой выборке/манифесте.
Имена для карточек нормализованы: исправлены очевидные повреждения буквы «С»
и добавлен контекст усадьбы. Исторические датировки и подробные тексты
из файла не перенесены в описания как актуальные факты.
Координаты новых карточек сохранены точно из GPX: это точки объектов,
а не подтверждённые пешеходные входы. Они ещё не проверены через Mapbox.

У всех 25 новых карточек часы и цены неизвестны; пустая цена не означает
бесплатный вход. Предполагается только внешний осмотр с общественного подхода,
без входа в здания, закрытые дворы, жильё и руины. Существование подхода,
состояние и сохранность объекта требуют проверки. `verified_at: 2026-10-13`
означает разбор предоставленного источника, без актуальной проверки посещения.
Приоритет 3 и длительность 10–40 минут — редакционные оценки. Не перенесены
рейтинги Globus в качестве измеренных оценок интереса пользователя.

Лошицкий парк остаётся общей прогулкой; девять отдельных построек имеют
собственные карточки. Планировщик пока не исключает смысловое пересечение
общей прогулки с отдельными осмотрами. Аналогичное пересечение возможно
у Верхнего города с его зданиями. Это ограничение текущей модели остановок.

## Сопоставление

| Индекс GPX | Объект в проекте | Решение |
| ---: | --- | --- |
| 300 | [upper-town](../data/places/minsk/upper-town.yaml) | Добавлен |
| 301 | [guest-court](../data/places/minsk/guest-court.yaml) | Добавлен |
| 302 | [masons-house](../data/places/minsk/masons-house.yaml) | Добавлен |
| 303 | [calvary-church](../data/places/minsk/calvary-church.yaml) | Добавлен |
| 304 | [red-church](../data/places/minsk/red-church.yaml) | Уже существовал; добавлен источник |
| 305 | [roch-church](../data/places/minsk/roch-church.yaml) | Уже существовал; добавлен источник |
| 306 | [holy-spirit-cathedral](../data/places/minsk/holy-spirit-cathedral.yaml) | Уже существовал; добавлен источник |
| 307 | [saint-joseph-church](../data/places/minsk/saint-joseph-church.yaml) | Добавлен |
| 308 | [mary-cathedral](../data/places/minsk/mary-cathedral.yaml) | Уже существовал; добавлен источник |
| 309 | [basilian-monastery-building](../data/places/minsk/basilian-monastery-building.yaml) | Добавлен |
| 310 | [basilian-convent-building](../data/places/minsk/basilian-convent-building.yaml) | Добавлен |
| 311 | [trinity-basilian-convent](../data/places/minsk/trinity-basilian-convent.yaml) | Добавлен |
| 312 | [boulder-museum](../data/places/minsk/boulder-museum.yaml) | Добавлен |
| 313 | [pishchalovsky-castle](../data/places/minsk/pishchalovsky-castle.yaml) | Добавлен |
| 314 | [town-hall](../data/places/minsk/town-hall.yaml) | Уже существовал; добавлен источник |
| 315 | [trinity-suburb](../data/places/minsk/trinity-suburb.yaml) | Уже существовал; добавлен источник |
| 316 | [white-dacha](../data/places/minsk/white-dacha.yaml) | Добавлен |
| 317 | [vankovich-large-slepyanka](../data/places/minsk/vankovich-large-slepyanka.yaml) | Добавлен |
| 318 | [vankovich-house](../data/places/minsk/vankovich-house.yaml) | Уже существовал; добавлен источник |
| 319 | [freedom-square-manor](../data/places/minsk/freedom-square-manor.yaml) | Добавлен |
| 320 | [alexander-nevsky-church](../data/places/minsk/alexander-nevsky-church.yaml) | Добавлен |
| 321 | [mary-magdalene-church](../data/places/minsk/mary-magdalene-church.yaml) | Добавлен |
| 322 | [peter-paul-cathedral](../data/places/minsk/peter-paul-cathedral.yaml) | Уже существовал; добавлен источник |
| 323 | [calvary-gate](../data/places/minsk/calvary-gate.yaml) | Добавлен |
| 1526 | [loshitsa-manor-house](../data/places/minsk/loshitsa-manor-house.yaml) | Добавлен |
| 1527 | [loshitsa-chapel](../data/places/minsk/loshitsa-chapel.yaml) | Добавлен |
| 1528 | [loshitsa-distillery](../data/places/minsk/loshitsa-distillery.yaml) | Добавлен |
| 1529 | [loshitsa-workers-house](../data/places/minsk/loshitsa-workers-house.yaml) | Добавлен |
| 1530 | [loshitsa-mill-ruins](../data/places/minsk/loshitsa-mill-ruins.yaml) | Добавлен |
| 1531 | [loshitsa-water-mill](../data/places/minsk/loshitsa-water-mill.yaml) | Добавлен |
| 1532 | [loshitsa-manager-house](../data/places/minsk/loshitsa-manager-house.yaml) | Добавлен |
| 1533 | [loshitsa-service-building](../data/places/minsk/loshitsa-service-building.yaml) | Добавлен |
| 1534 | [loshitsa-gate](../data/places/minsk/loshitsa-gate.yaml) | Добавлен |

## Воспроизведение

Из корня репозитория, при наличии исходного пользовательского файла:

```bash
python3 scripts/import_globus_gpx.py ../globus.by.gpx
```

Скрипт работает без сети и БД, проверяет SHA-256 конкретной выгрузки,
сохраняет ранее созданные карточки при повторном запуске и не применяет
инструкции из текстов GPX. Другую выгрузку нужно сначала сопоставить заново.
Для загрузки готовых карточек в БД используется штатный импортёр:

```bash
cd backend
# POSTGRES_DSN должен указывать на локальную БД проекта
go run ./cmd/import -check-links=false ../data/places/minsk
```

## Проверка результата

Штатный Go-импортёр принял все 60 карточек. Локальная PostgreSQL запущена;
в основную БД импортировано 60 активных мест с уникальными slug.
Два последовательных импорта сохранили одинаковые ID всех мест.
Повторный запуск GPX-скрипта сохранил байтовое содержимое всех 60 YAML.
Сверены точные координаты всех 25 новых карточек и неизвестные часы/цены.
Go-тесты с `-race`, `go vet`, gofmt и `git diff --check` прошли.
Проверка ссылок при импорте отключена: она не подтверждает запись из GPX.
Mapbox и полевой обход для новых точек не запускались.
